package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	searchAliasTreatments = "melt-tratamientos-v1-read"
	searchAliasPeople     = "melt-profesionales-v1-read"
	cacheTTL              = 30 * time.Second
	searchFreshnessTarget = 60 * time.Second
)

type searchClient struct {
	base              string
	http              *http.Client
	treatmentAlias    string
	professionalAlias string
}

type searchHTTPError int

func (e searchHTTPError) Error() string { return fmt.Sprintf("opensearch returned HTTP %d", int(e)) }

func newSearchClient(raw string) *searchClient {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil
	}
	return &searchClient{base: raw, http: &http.Client{Timeout: 4 * time.Second}, treatmentAlias: searchAliasTreatments, professionalAlias: searchAliasPeople}
}

func (s *searchClient) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, searchHTTPError(resp.StatusCode)
	}
	return data, nil
}

func (s *searchClient) bulk(ctx context.Context, body []byte) error {
	data, err := s.request(ctx, http.MethodPost, "/_bulk?refresh=true", body)
	if err != nil {
		return err
	}
	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if result.Errors {
		return errors.New("opensearch bulk indexing reported item failures")
	}
	return nil
}

func (s *Servidor) runIndexer(ctx context.Context) {
	if s.search == nil {
		return
	}
	if err := s.refreshIndexFreshness(ctx); err != nil {
		slogWarn("catalog_index_freshness_read_failed", err)
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			acquired, owner, leaseErr := s.acquireIndexerLease(ctx)
			if leaseErr != nil {
				s.indexFailures.Add(1)
				slogWarn("catalog_index_lease_failed", leaseErr)
			}
			if acquired {
				err := s.rebuildSearchIndices(ctx)
				s.releaseIndexerLease(owner)
				if err != nil {
					s.indexFailures.Add(1)
					slogWarn("catalog_index_rebuild_failed", err)
				}
			} else if leaseErr == nil {
				if refreshErr := s.refreshIndexFreshness(ctx); refreshErr != nil {
					slogWarn("catalog_index_freshness_read_failed", refreshErr)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Servidor) refreshIndexFreshness(ctx context.Context) error {
	if s.control == nil {
		return errors.New("catalog control collection is unavailable")
	}
	var state struct {
		LastSuccessAt time.Time `bson:"lastSuccessAt"`
	}
	err := s.control.FindOne(ctx, bson.M{"_id": "opensearch_indexer"}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	if !state.LastSuccessAt.IsZero() {
		s.lastIndexSuccess.Store(state.LastSuccessAt.Unix())
	}
	return nil
}

func (s *Servidor) searchIndexFresh() bool {
	last := s.lastIndexSuccess.Load()
	return last > 0 && time.Since(time.Unix(last, 0)) <= searchFreshnessTarget
}

func (s *Servidor) acquireIndexerLease(ctx context.Context) (bool, string, error) {
	if s.control == nil {
		return false, "", errors.New("catalog control collection is unavailable")
	}
	now := time.Now().UTC()
	owner := strconv.FormatInt(now.UnixNano(), 10)
	filter := bson.M{"_id": "opensearch_indexer", "$or": bson.A{bson.M{"leaseUntil": bson.M{"$lt": now}}, bson.M{"leaseUntil": bson.M{"$exists": false}}}}
	update := bson.M{"$set": bson.M{"leaseUntil": now.Add(40 * time.Second), "leaseOwner": owner}}
	err := s.control.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetUpsert(true)).Err()
	if mongo.IsDuplicateKeyError(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, owner, nil
}

func (s *Servidor) releaseIndexerLease(owner string) {
	if s.control == nil || owner == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.control.UpdateOne(ctx, bson.M{"_id": "opensearch_indexer", "leaseOwner": owner}, bson.M{"$set": bson.M{"leaseUntil": time.Now().UTC()}}); err != nil {
		slogWarn("catalog_index_lease_release_failed", err)
	}
}

func (s *Servidor) rebuildSearchIndices(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 27*time.Second)
	defer cancel()
	suffix := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	treatmentsIndex := strings.TrimSuffix(s.search.treatmentAlias, "-read") + "-" + suffix
	peopleIndex := strings.TrimSuffix(s.search.professionalAlias, "-read") + "-" + suffix
	for name, mapping := range map[string]string{
		treatmentsIndex: `{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":{"properties":{"id":{"type":"keyword"},"nombre":{"type":"text","fields":{"keyword":{"type":"keyword"}}},"descripcion":{"type":"text"},"keywords":{"type":"text"},"categoria":{"type":"text","fields":{"keyword":{"type":"keyword"}}},"precio":{"type":"double"},"moneda":{"type":"keyword"},"duracionMinutos":{"type":"integer"},"imagenes":{"type":"keyword","index":false},"profesionalesIds":{"type":"keyword"}}}}`,
		peopleIndex:     `{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":{"properties":{"id":{"type":"keyword"},"nombre":{"type":"text","fields":{"keyword":{"type":"keyword"}}},"descripcion":{"type":"text"},"tratamientosIds":{"type":"keyword"},"activo":{"type":"boolean"}}}}`,
	} {
		if _, err := s.search.request(ctx, http.MethodPut, "/"+name, []byte(mapping)); err != nil {
			return err
		}
	}
	if err := bulkIndexTreatments(ctx, s.search, treatmentsIndex, s.coleccion); err != nil {
		return err
	}
	if err := bulkIndexPeople(ctx, s.search, peopleIndex, s.profesionales); err != nil {
		return err
	}
	if err := s.search.switchAlias(ctx, s.search.treatmentAlias, treatmentsIndex); err != nil {
		return err
	}
	if err := s.search.switchAlias(ctx, s.search.professionalAlias, peopleIndex); err != nil {
		return err
	}
	if err := s.search.cleanupIndices(ctx, strings.TrimSuffix(s.search.treatmentAlias, "-read")+"-*", strings.TrimSuffix(s.search.professionalAlias, "-read")+"-*", treatmentsIndex, peopleIndex); err != nil {
		slogWarn("catalog_index_cleanup_failed", err)
	}
	if s.cache != nil {
		if err := s.cache.bumpGeneration(); err != nil {
			slogWarn("catalog_cache_invalidation_failed", err)
		}
	}
	now := time.Now().UTC()
	if _, err := s.control.UpdateOne(ctx, bson.M{"_id": "opensearch_indexer"}, bson.M{"$set": bson.M{"lastSuccessAt": now}}, options.UpdateOne().SetUpsert(true)); err != nil {
		return err
	}
	s.lastIndexSuccess.Store(now.Unix())
	return nil
}

func (s *searchClient) switchAlias(ctx context.Context, alias, target string) error {
	data, err := s.request(ctx, http.MethodGet, "/_alias/"+alias, nil)
	var old map[string]json.RawMessage
	if err == nil {
		if err := json.Unmarshal(data, &old); err != nil {
			return err
		}
	} else {
		var status searchHTTPError
		if !errors.As(err, &status) || status != http.StatusNotFound {
			return err
		}
	}
	actions := make([]any, 0, len(old)+1)
	for index := range old {
		actions = append(actions, map[string]any{"remove": map[string]any{"index": index, "alias": alias}})
	}
	actions = append(actions, map[string]any{"add": map[string]any{"index": target, "alias": alias}})
	body, err := json.Marshal(map[string]any{"actions": actions})
	if err != nil {
		return err
	}
	_, err = s.request(ctx, http.MethodPost, "/_aliases", body)
	return err
}

func (s *searchClient) deleteAliasAndIndices(ctx context.Context, alias, pattern string) error {
	data, err := s.request(ctx, http.MethodGet, "/_alias/"+alias, nil)
	var indices map[string]json.RawMessage
	if err == nil {
		if err := json.Unmarshal(data, &indices); err != nil {
			return err
		}
		actions := make([]any, 0, len(indices))
		for index := range indices {
			actions = append(actions, map[string]any{"remove": map[string]any{"index": index, "alias": alias}})
		}
		body, err := json.Marshal(map[string]any{"actions": actions})
		if err != nil {
			return err
		}
		if _, err := s.request(ctx, http.MethodPost, "/_aliases", body); err != nil {
			return err
		}
		for index := range indices {
			if strings.HasPrefix(index, strings.TrimSuffix(pattern, "*")) {
				if _, err := s.request(ctx, http.MethodDelete, "/"+index, nil); err != nil {
					return err
				}
			}
		}
	} else {
		var status searchHTTPError
		if !errors.As(err, &status) || status != http.StatusNotFound {
			return err
		}
	}
	return s.cleanupIndices(ctx, pattern, pattern)
}

func (s *searchClient) cleanupIndices(ctx context.Context, treatmentPattern, peoplePattern string, active ...string) error {
	activeSet := map[string]bool{}
	for _, name := range active {
		activeSet[name] = true
	}
	for _, prefix := range []string{treatmentPattern, peoplePattern} {
		data, err := s.request(ctx, http.MethodGet, "/_cat/indices/"+prefix+"?format=json&h=index", nil)
		if err != nil {
			return err
		}
		var indices []struct {
			Index string `json:"index"`
		}
		if err := json.Unmarshal(data, &indices); err != nil {
			return err
		}
		for _, item := range indices {
			if item.Index != "" && !activeSet[item.Index] {
				if _, err := s.request(ctx, http.MethodDelete, "/"+item.Index, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func bulkIndexTreatments(ctx context.Context, search *searchClient, index string, col *mongo.Collection) error {
	cur, err := col.Find(ctx, bson.M{"eliminado": bson.M{"$ne": true}})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	var batch bytes.Buffer
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		e := search.bulk(ctx, batch.Bytes())
		batch.Reset()
		return e
	}
	for cur.Next(ctx) {
		var item Tratamiento
		if err := cur.Decode(&item); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"index": map[string]string{"_index": index, "_id": item.ID}})
		doc, err := json.Marshal(item)
		if err != nil {
			return err
		}
		batch.Write(meta)
		batch.WriteByte('\n')
		batch.Write(doc)
		batch.WriteByte('\n')
		if batch.Len() >= 4<<20 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return err
	}
	return flush()
}

func bulkIndexPeople(ctx context.Context, search *searchClient, index string, col *mongo.Collection) error {
	cur, err := col.Find(ctx, bson.M{"activo": true})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	var batch bytes.Buffer
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		e := search.bulk(ctx, batch.Bytes())
		batch.Reset()
		return e
	}
	for cur.Next(ctx) {
		var item Profesional
		if err := cur.Decode(&item); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"index": map[string]string{"_index": index, "_id": item.ID}})
		doc, err := json.Marshal(item)
		if err != nil {
			return err
		}
		batch.Write(meta)
		batch.WriteByte('\n')
		batch.Write(doc)
		batch.WriteByte('\n')
		if batch.Len() >= 4<<20 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return err
	}
	return flush()
}

type searchPage struct {
	Treatments []Tratamiento `json:"tratamientos"`
	Total      int64         `json:"total"`
}

func (s *searchClient) searchTreatments(ctx context.Context, q, category string, maxPrice float64, hasMax bool, page, limit, direction int, sortField string) (searchPage, error) {
	filters := []any{}
	if category != "" {
		filters = append(filters, map[string]any{"match_phrase": map[string]any{"categoria": category}})
	}
	if hasMax {
		filters = append(filters, map[string]any{"range": map[string]any{"precio": map[string]any{"lte": maxPrice}}})
	}
	sortKey := sortField
	if sortField == "nombre" {
		sortKey = "nombre.keyword"
	}
	body := map[string]any{"from": (page - 1) * limit, "size": limit, "query": map[string]any{"bool": map[string]any{"must": []any{map[string]any{"multi_match": map[string]any{"query": q, "fields": []string{"nombre^3", "descripcion", "keywords"}, "type": "best_fields"}}}, "filter": filters}}, "sort": []any{map[string]any{sortKey: map[string]any{"order": map[bool]string{true: "desc", false: "asc"}[direction < 0]}}, map[string]any{"id": "asc"}}}
	data, err := json.Marshal(body)
	if err != nil {
		return searchPage{}, err
	}
	resp, err := s.request(ctx, http.MethodPost, "/"+s.treatmentAlias+"/_search", data)
	if err != nil {
		return searchPage{}, err
	}
	var raw struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source Tratamiento `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(resp, &raw); err != nil {
		return searchPage{}, err
	}
	items := make([]Tratamiento, 0, len(raw.Hits.Hits))
	for _, hit := range raw.Hits.Hits {
		normalizarLecturaTratamiento(&hit.Source)
		items = append(items, hit.Source)
	}
	return searchPage{Treatments: items, Total: raw.Hits.Total.Value}, nil
}

type catalogListQuery struct {
	Q        string `json:"q"`
	Category string `json:"categoria"`
	MaxPrice string `json:"precioMax"`
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
	Sort     string `json:"sort"`
	Order    string `json:"order"`
}

type memcacheClient struct {
	address              string
	namespace            string
	timeout              time.Duration
	hits, misses, errors atomic.Uint64
}

func newMemcacheClient(address string) *memcacheClient {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	namespace := strings.TrimSpace(os.Getenv("MEMCACHED_NAMESPACE"))
	if namespace == "" {
		namespace = "melt:catalogo"
	}
	return &memcacheClient{address: address, namespace: namespace, timeout: 50 * time.Millisecond}
}
func (m *memcacheClient) key(query catalogListQuery) (string, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return m.namespace + ":v1:" + hex.EncodeToString(hash[:]), nil
}
func (m *memcacheClient) dial() (net.Conn, *bufio.Reader, error) {
	conn, err := net.DialTimeout("tcp", m.address, m.timeout)
	if err != nil {
		m.errors.Add(1)
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(m.timeout))
	return conn, bufio.NewReader(conn), nil
}
func (m *memcacheClient) generation() (string, error) {
	key := m.namespace + ":generation"
	v, ok, err := m.getRaw(key)
	if err != nil {
		return "", err
	}
	if ok {
		return strings.TrimSpace(string(v)), nil
	}
	token, err := newCacheGeneration()
	if err != nil {
		return "", err
	}
	if err := m.addRaw(key, []byte(token), 0); err != nil {
		if errors.Is(err, errExists) {
			v, ok, err = m.getRaw(key)
			if err == nil && ok {
				return strings.TrimSpace(string(v)), nil
			}
		}
		return "", err
	}
	return token, nil
}

func newCacheGeneration() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
func (m *memcacheClient) getRaw(key string) ([]byte, bool, error) {
	c, r, e := m.dial()
	if e != nil {
		return nil, false, e
	}
	defer c.Close()
	_, e = fmt.Fprintf(c, "get %s\r\n", key)
	if e != nil {
		m.errors.Add(1)
		return nil, false, e
	}
	line, e := r.ReadString('\n')
	if e != nil {
		m.errors.Add(1)
		return nil, false, e
	}
	if line == "END\r\n" {
		return nil, false, nil
	}
	parts := strings.Fields(line)
	if len(parts) != 4 || parts[0] != "VALUE" {
		m.errors.Add(1)
		return nil, false, errors.New("invalid memcached response")
	}
	n, e := strconv.Atoi(parts[3])
	if e != nil || n < 0 || n > 8<<20 {
		m.errors.Add(1)
		return nil, false, errors.New("invalid memcached value size")
	}
	b := make([]byte, n+2)
	if _, e = io.ReadFull(r, b); e != nil {
		m.errors.Add(1)
		return nil, false, e
	}
	if string(b[n:]) != "\r\n" {
		m.errors.Add(1)
		return nil, false, errors.New("invalid memcached value")
	}
	if _, e = r.ReadString('\n'); e != nil {
		m.errors.Add(1)
		return nil, false, e
	}
	return b[:n], true, nil
}

func (m *memcacheClient) getRawMany(keys ...string) (map[string][]byte, error) {
	values := make(map[string][]byte, len(keys))
	c, r, err := m.dial()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err := fmt.Fprintf(c, "get %s\r\n", strings.Join(keys, " ")); err != nil {
		m.errors.Add(1)
		return nil, err
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			m.errors.Add(1)
			return nil, err
		}
		if line == "END\r\n" {
			return values, nil
		}
		parts := strings.Fields(line)
		if len(parts) != 4 || parts[0] != "VALUE" {
			m.errors.Add(1)
			return nil, errors.New("invalid memcached response")
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil || n < 0 || n > 8<<20 {
			m.errors.Add(1)
			return nil, errors.New("invalid memcached value size")
		}
		value := make([]byte, n+2)
		if _, err := io.ReadFull(r, value); err != nil {
			m.errors.Add(1)
			return nil, err
		}
		if string(value[n:]) != "\r\n" {
			m.errors.Add(1)
			return nil, errors.New("invalid memcached value")
		}
		values[parts[1]] = value[:n]
	}
}

var errExists = errors.New("memcached key exists")

func (m *memcacheClient) setRaw(key string, value []byte, ttl time.Duration) error {
	c, _, e := m.dial()
	if e != nil {
		return e
	}
	defer c.Close()
	if _, e = fmt.Fprintf(c, "set %s 0 %d %d\r\n", key, int(ttl.Seconds()), len(value)); e == nil {
		_, e = c.Write(append(value, '\r', '\n'))
	}
	if e != nil {
		m.errors.Add(1)
		return e
	}
	line, e := bufio.NewReader(c).ReadString('\n')
	if e != nil || line != "STORED\r\n" {
		m.errors.Add(1)
		return errors.New("memcached set failed")
	}
	return nil
}
func (m *memcacheClient) addRaw(key string, value []byte, ttl time.Duration) error {
	c, _, e := m.dial()
	if e != nil {
		return e
	}
	defer c.Close()
	fmt.Fprintf(c, "add %s 0 %d %d\r\n%s\r\n", key, int(ttl.Seconds()), len(value), value)
	line, e := bufio.NewReader(c).ReadString('\n')
	if e != nil {
		m.errors.Add(1)
		return e
	}
	if line == "NOT_STORED\r\n" {
		return errExists
	}
	if line != "STORED\r\n" {
		m.errors.Add(1)
		return errors.New("memcached add failed")
	}
	return nil
}
func (m *memcacheClient) bumpGeneration() error {
	token, err := newCacheGeneration()
	if err != nil {
		return err
	}
	return m.setRaw(m.namespace+":generation", []byte(token), 0)
}
func (m *memcacheClient) get(query catalogListQuery) ([]byte, bool) {
	key, e := m.key(query)
	if e != nil {
		m.errors.Add(1)
		return nil, false
	}
	generationKey := m.namespace + ":generation"
	values, e := m.getRawMany(generationKey, key)
	if e != nil {
		m.errors.Add(1)
		return nil, false
	}
	generation, exists := values[generationKey]
	if !exists {
		if addErr := m.addRaw(generationKey, []byte("1"), 0); addErr != nil && !errors.Is(addErr, errExists) {
			return nil, false
		}
		values, e = m.getRawMany(generationKey, key)
		if e != nil {
			m.errors.Add(1)
			return nil, false
		}
		generation = values[generationKey]
	}
	value, ok := values[key]
	if ok {
		version, payload, found := bytes.Cut(value, []byte{'\n'})
		if found && bytes.Equal(version, generation) {
			m.hits.Add(1)
			return payload, true
		}
	}
	m.misses.Add(1)
	return nil, false
}
func (m *memcacheClient) set(query catalogListQuery, value []byte) {
	key, e := m.key(query)
	if e != nil {
		m.errors.Add(1)
		return
	}
	generation, err := m.generation()
	if err != nil {
		return
	}
	envelope := append(append([]byte(generation), '\n'), value...)
	_ = m.setRaw(key, envelope, cacheTTL)
}
func (m *memcacheClient) stats() map[string]uint64 {
	return map[string]uint64{"hits": m.hits.Load(), "misses": m.misses.Load(), "errors": m.errors.Load()}
}

func slogWarn(message string, err error) { slog.Warn(message, "error", err) }

func (s *Servidor) metrics(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if s == nil {
		c.String(http.StatusOK, "catalog_cache_hits_total 0\ncatalog_cache_misses_total 0\ncatalog_cache_errors_total 0\ncatalog_search_failures_total 0\ncatalog_index_rebuild_failures_total 0\ncatalog_index_last_success_timestamp_seconds 0\n")
		return
	}
	var hits, misses, cacheErrors uint64
	if s.cache != nil {
		stats := s.cache.stats()
		hits, misses, cacheErrors = stats["hits"], stats["misses"], stats["errors"]
	}
	fmt.Fprintf(c.Writer, "catalog_cache_hits_total %d\ncatalog_cache_misses_total %d\ncatalog_cache_errors_total %d\ncatalog_search_failures_total %d\ncatalog_index_rebuild_failures_total %d\ncatalog_index_last_success_timestamp_seconds %d\ncatalog_index_freshness_target_seconds 60\n", hits, misses, cacheErrors, s.searchFailures.Load(), s.indexFailures.Load(), s.lastIndexSuccess.Load())
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type identidad struct {
	ID string `json:"id"`
	Email string `json:"email"`
	Roles []string `json:"roles"`
}

type agendaSemanal struct {
	ID string `bson:"_id" json:"id"`
	ProfesionalID string `bson:"profesionalId" json:"profesionalId"`
	DiaSemana int `bson:"diaSemana" json:"diaSemana"`
	HoraInicio string `bson:"horaInicio" json:"horaInicio"`
	HoraFin string `bson:"horaFin" json:"horaFin"`
	Activo bool `bson:"activo" json:"activo"`
}

type turno struct {
	ID string `bson:"_id" json:"id"`
	ClienteID string `bson:"clienteId" json:"clienteId"`
	ProfesionalID string `bson:"profesionalId" json:"profesionalId"`
	TratamientoID string `bson:"tratamientoId" json:"tratamientoId"`
	Inicio time.Time `bson:"inicio" json:"inicio"`
	Fin time.Time `bson:"fin" json:"fin"`
	Estado string `bson:"estado" json:"estado"`
	CreadoEn time.Time `bson:"creadoEn" json:"creadoEn"`
	PrecioTratamiento float64 `bson:"precioTratamiento" json:"precioTratamiento"`
	Sena float64 `bson:"sena" json:"sena"`
	PagoID string `bson:"pagoId,omitempty" json:"pagoId,omitempty"`
	EstadoPago string `bson:"estadoPago,omitempty" json:"estadoPago,omitempty"`
	ImporteDevolucion float64 `bson:"importeDevolucion,omitempty" json:"importeDevolucion,omitempty"`
	EstadoDevolucion string `bson:"estadoDevolucion,omitempty" json:"estadoDevolucion,omitempty"`
}

type servidorReservas struct {
	agendas *mongo.Collection
	turnos *mongo.Collection
	bloqueos *mongo.Collection
	carritos *mongo.Collection
	catalogoURL string
	pagosURL string
	httpClient *http.Client
}

type tratamientoCatalogo struct {
	ID string `json:"id"`
	Nombre string `json:"nombre"`
	DuracionMinutos int `json:"duracionMinutos"`
	ProfesionalesIDs []string `json:"profesionalesIds"`
	Precio float64 `json:"precio"`
	Moneda string `json:"moneda"`
}

func tratamientoCompatible(t tratamientoCatalogo, profesionalID string) bool {
	profesionalID = strings.TrimSpace(profesionalID)
	if profesionalID == "" { return false }
	for _, id := range t.ProfesionalesIDs { if strings.TrimSpace(id) == profesionalID { return true } }
	return false
}

type bloqueoTurno struct {
	ID string `bson:"_id,omitempty"`
	ProfesionalID string `bson:"profesionalId"`
	Minuto int64 `bson:"minuto"`
	TurnoID string `bson:"turnoId"`
	ExpiraEn *time.Time `bson:"expiraEn,omitempty"`
}

func (s *servidorReservas) obtenerTratamiento(ctx context.Context, id string) (tratamientoCatalogo, int, error) {
	var t tratamientoCatalogo
	if strings.TrimSpace(id) == "" { return t, http.StatusBadRequest, errors.New("tratamientoId es obligatorio") }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.catalogoURL, "/")+"/tratamientos/"+url.PathEscape(id), nil)
	if err != nil { return t, http.StatusServiceUnavailable, err }
	resp, err := s.httpClient.Do(req)
	if err != nil { return t, http.StatusServiceUnavailable, err }
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound { return t, http.StatusUnprocessableEntity, errors.New("el tratamiento no existe en Catálogo") }
	if resp.StatusCode != http.StatusOK { return t, http.StatusServiceUnavailable, errors.New("Catálogo no está disponible") }
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil || t.ID == "" || t.DuracionMinutos < 1 || t.DuracionMinutos > 480 {
		return t, http.StatusServiceUnavailable, errors.New("Catálogo devolvió un tratamiento inválido")
	}
	return t, http.StatusOK, nil
}

var permisosPorOperacion = map[string]map[string]bool{
	"reservas.consultar-propias": {"cliente": true},
	"reservas.consultar-disponibilidad": {"cliente": true, "profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.consultar-mis-turnos": {"cliente": true, "profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.cancelar-turno": {"cliente": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.crear": {"cliente": true},
	"reservas.cancelar-propia": {"cliente": true},
	"reservas.consultar-agenda": {"profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.gestionar-agenda": {"profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.consultar-todas": {"recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.gestionar-turnos": {"recepcion": true, "administrador": true, "superadministrador": true},
}

func main() {
	uri := strings.TrimSpace(os.Getenv("MONGODB_URI"))
	if uri == "" { uri = "mongodb://localhost:27017" }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil { log.Fatal("no se pudo configurar MongoDB") }
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil { log.Fatal("no se pudo conectar con MongoDB") }
	db := client.Database("reservas_db")
	catalogoURL := strings.TrimSpace(os.Getenv("CATALOGO_URL"))
	if catalogoURL == "" { catalogoURL = "http://catalogo-profesionales:8081" }
	s := &servidorReservas{agendas: db.Collection("agendas_semanales"), turnos: db.Collection("turnos"), bloqueos: db.Collection("bloqueos_turnos"), carritos: db.Collection("carritos_bloqueos"), catalogoURL: catalogoURL, pagosURL: strings.TrimRight(strings.TrimSpace(os.Getenv("PAGOS_URL")), "/"), httpClient: &http.Client{Timeout: 3*time.Second}}
	_, err = s.agendas.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"profesionalId",Value:1},{Key:"diaSemana",Value:1}}, Options: options.Index().SetUnique(true).SetName("uq_agenda_profesional_dia")})
	if err != nil { log.Fatal("no se pudo crear índice de agenda") }
	_, err = s.turnos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"profesionalId",Value:1},{Key:"inicio",Value:1}}, Options: options.Index().SetName("ix_turnos_profesional_inicio")})
	if err != nil { log.Fatal("no se pudo crear índice de turnos") }
	_, err = s.bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"profesionalId",Value:1},{Key:"minuto",Value:1}}, Options: options.Index().SetUnique(true).SetName("uq_bloqueo_profesional_minuto")})
	if err != nil { log.Fatal("no se pudo crear índice de bloqueos de turnos") }
	_, err = s.bloqueos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"expiraEn",Value:1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_bloqueos_carrito")})
	if err != nil { log.Fatal("no se pudo crear índice TTL de bloqueos") }
	_, err = s.carritos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"expiraEn",Value:1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_carritos")})
	if err != nil { log.Fatal("no se pudo crear índice TTL de carritos") }
	if err := s.router().Run(":8082"); err != nil { log.Fatal("no se pudo iniciar Reservas") }
}

func (s *servidorReservas) router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"servicio":"reservas","estado":"ok"}) })
	r.GET("/me", requiereIdentidad(), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"usuario": identidadDesdeHeaders(c)}) })
	r.PUT("/agenda", requiereIdentidad("profesional","recepcion","administrador","superadministrador"), requierePermiso("reservas.gestionar-agenda"), s.guardarAgenda)
	r.GET("/disponibilidad", requiereIdentidad("cliente","profesional","recepcion","administrador","superadministrador"), requierePermiso("reservas.consultar-disponibilidad"), s.disponibilidad)
	r.GET("/turnos", requiereIdentidad(), requierePermiso("reservas.consultar-mis-turnos"), s.listarTurnos)
	r.POST("/carrito/bloqueos", requiereIdentidad("cliente"), requierePermiso("reservas.crear"), s.crearBloqueoCarrito)
	r.POST("/carrito/bloqueos/:id/confirmar", requiereIdentidad("cliente"), requierePermiso("reservas.crear"), s.confirmarBloqueoCarrito)
	r.DELETE("/carrito/bloqueos/:id", requiereIdentidad("cliente"), requierePermiso("reservas.crear"), s.liberarBloqueoCarrito)
	r.POST("/turnos", requiereIdentidad("cliente"), requierePermiso("reservas.crear"), s.confirmarTurnoDesdeCarrito)
	r.DELETE("/turnos/:id", requiereIdentidad(), requierePermiso("reservas.cancelar-turno"), s.cancelarTurno)
	r.POST("/turnos/:id/devolucion/reintentar", requiereIdentidad(), requierePermiso("reservas.cancelar-turno"), s.reintentarDevolucion)
	return r
}

func identidadDesdeHeaders(c *gin.Context) identidad {
	roles := []string{}
	for _, rol := range strings.Split(c.GetHeader("X-Auth-User-Roles"), ",") {
		if rol = strings.TrimSpace(rol); rol != "" { roles = append(roles, rol) }
	}
	return identidad{ID:strings.TrimSpace(c.GetHeader("X-Auth-User-ID")), Email:strings.ToLower(strings.TrimSpace(c.GetHeader("X-Auth-User-Email"))), Roles:roles}
}

func tieneRol(u identidad, roles ...string) bool {
	for _, actual := range u.Roles { for _, esperado := range roles { if actual == esperado { return true } } }
	return false
}

func requiereIdentidad(rolesPermitidos ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := identidadDesdeHeaders(c)
		if u.ID == "" || u.Email == "" || len(u.Roles) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"IDENTIDAD_REQUERIDA","message":"Iniciá sesión para continuar."}})
			return
		}
		if len(rolesPermitidos)>0 && !tieneRol(u, rolesPermitidos...) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error":gin.H{"code":"PERMISOS_INSUFICIENTES","message":"No tenés permisos para realizar esta acción."}})
			return
		}
		c.Set("identidad",u)
		c.Next()
	}
}

func requierePermiso(operacion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get("identidad")
		u, valid := v.(identidad)
		if !ok || !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error":gin.H{"code":"IDENTIDAD_REQUERIDA","message":"Iniciá sesión para continuar."}})
			return
		}
		permitidos, exists := permisosPorOperacion[operacion]
		if !exists || !tieneRol(u, mapKeys(permitidos)... ) {
			code := "PERMISOS_INSUFICIENTES"
			if !exists { code = "OPERACION_SIN_PERMISO" }
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error":gin.H{"code":code,"message":"No tenés permisos para realizar esta acción."}})
			return
		}
		c.Next()
	}
}

func mapKeys(m map[string]bool) []string { out:=make([]string,0,len(m)); for k:=range m { if m[k] { out=append(out,k) } }; return out }

type agendaRequest struct {
	ProfesionalID string `json:"profesionalId"`
	DiaSemana int `json:"diaSemana"`
	HoraInicio string `json:"horaInicio"`
	HoraFin string `json:"horaFin"`
	Activo *bool `json:"activo"`
}

func parseHora(s string) (int,error) {
	t,err:=time.Parse("15:04",s); if err!=nil { return 0,err }; return t.Hour()*60+t.Minute(),nil
}
func agendaValida(dia int, inicio, fin string) bool {
	if dia<0 || dia>6 { return false }
	a,errA:=parseHora(inicio); b,errB:=parseHora(fin)
	return errA==nil && errB==nil && a<b
}
func (s *servidorReservas) guardarAgenda(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	var req agendaRequest
	if err:=c.ShouldBindJSON(&req); err!=nil { c.JSON(http.StatusBadRequest, gin.H{"error":"JSON inválido"}); return }
	profesionalID:=strings.TrimSpace(req.ProfesionalID)
	if tieneRol(u,"profesional") && !tieneRol(u,"recepcion","administrador","superadministrador") { profesionalID=u.ID }
	if profesionalID=="" || !agendaValida(req.DiaSemana,req.HoraInicio,req.HoraFin) {
		c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"AGENDA_INVALIDA","message":"Indicá profesional, día de 0 (domingo) a 6 (sábado) y un rango horario válido."}}); return
	}
	activo:=true; if req.Activo!=nil { activo=*req.Activo }
	doc:=agendaSemanal{ID:profesionalID+"-"+time.Weekday(req.DiaSemana).String(),ProfesionalID:profesionalID,DiaSemana:req.DiaSemana,HoraInicio:req.HoraInicio,HoraFin:req.HoraFin,Activo:activo}
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	_,err:=s.agendas.UpdateOne(ctx,bson.D{{Key:"profesionalId",Value:profesionalID},{Key:"diaSemana",Value:req.DiaSemana}},bson.D{{Key:"$set",Value:doc}},options.UpdateOne().SetUpsert(true))
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo guardar la agenda"}); return }
	c.JSON(http.StatusOK,gin.H{"agenda":doc})
}

func (s *servidorReservas) disponibilidad(c *gin.Context) {
	prof:=strings.TrimSpace(c.Query("profesionalId")); fecha,err:=time.Parse("2006-01-02",c.Query("fecha"))
	if prof=="" || err!=nil { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"PARAMETROS_INVALIDOS","message":"Usá profesionalId y fecha (YYYY-MM-DD)."}}); return }
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	dur := 0
	if tratamientoID := strings.TrimSpace(c.Query("tratamientoId")); tratamientoID != "" {
		tratamiento, status, catErr := s.obtenerTratamiento(ctx, tratamientoID)
		if catErr != nil { c.JSON(status, gin.H{"error":gin.H{"code":"TRATAMIENTO_NO_VALIDO","message":catErr.Error()}}); return }
		dur = tratamiento.DuracionMinutos
	} else {
		// Compatibilidad temporal: los clientes nuevos deben enviar tratamientoId.
		dur, err = parsePositiveInt(c.Query("duracionMinutos"))
		if err != nil { dur = 0 }
	}
	if dur < 5 || dur > 480 { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"PARAMETROS_INVALIDOS","message":"Enviá tratamientoId (preferido) o duracionMinutos entre 5 y 480."}}); return }
	var agenda agendaSemanal
	if err:=s.agendas.FindOne(ctx,bson.D{{Key:"profesionalId",Value:prof},{Key:"diaSemana",Value:int(fecha.Weekday())},{Key:"activo",Value:true}}).Decode(&agenda); err!=nil {
		if errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusOK,gin.H{"profesionalId":prof,"fecha":c.Query("fecha"),"horarios":[]string{}}); return }
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo consultar la agenda"}); return
	}
	startMin,_:=parseHora(agenda.HoraInicio); endMin,_:=parseHora(agenda.HoraFin)
	loc:=fecha.Location(); base:=time.Date(fecha.Year(),fecha.Month(),fecha.Day(),0,0,0,0,loc)
	var slots []string
	for m:=startMin; m+dur<=endMin; m+=15 {
		start:=base.Add(time.Duration(m)*time.Minute); end:=start.Add(time.Duration(dur)*time.Minute)
		var count int64
		count,err=s.turnos.CountDocuments(ctx,bson.D{{Key:"profesionalId",Value:prof},{Key:"estado",Value:"confirmado"},{Key:"inicio",Value:bson.D{{Key:"$lt",Value:end}}},{Key:"fin",Value:bson.D{{Key:"$gt",Value:start}}}})
		if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo consultar disponibilidad"}); return }
		if count==0 && start.After(time.Now()) { slots=append(slots,start.Format("15:04")) }
	}
	if slots==nil { slots=[]string{} }
	c.JSON(http.StatusOK,gin.H{"profesionalId":prof,"fecha":c.Query("fecha"),"duracionMinutos":dur,"horarios":slots})
}

func parsePositiveInt(v string) (int,error) { var n int; _,err:=fmtSscanf(v,"%d",&n); if err!=nil || n<=0 { return 0,errors.New("entero inválido") }; return n,nil }

func (s *servidorReservas) crearTurno(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	var req struct { ProfesionalID string `json:"profesionalId" binding:"required"`; TratamientoID string `json:"tratamientoId" binding:"required"`; Inicio string `json:"inicio" binding:"required"` }
	if err:=c.ShouldBindJSON(&req); err!=nil { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"TURNO_INVALIDO","message":"Indicá profesionalId, tratamientoId e inicio RFC3339."}}); return }
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	tratamiento, status, catErr := s.obtenerTratamiento(ctx, req.TratamientoID)
	if catErr != nil { c.JSON(status, gin.H{"error":gin.H{"code":"TRATAMIENTO_NO_VALIDO","message":catErr.Error()}}); return }
	if !tratamientoCompatible(tratamiento, req.ProfesionalID) { c.JSON(http.StatusUnprocessableEntity, gin.H{"error":gin.H{"code":"PROFESIONAL_NO_APTO","message":"El profesional seleccionado no está habilitado para realizar este tratamiento."}}); return }
	start,err:=time.Parse(time.RFC3339,req.Inicio); if err!=nil || !start.After(time.Now()) { c.JSON(http.StatusBadRequest,gin.H{"error":"el inicio debe ser una fecha futura RFC3339"}); return }
	duracionMinutos := tratamiento.DuracionMinutos
	end:=start.Add(time.Duration(duracionMinutos)*time.Minute)
	if start.Second()!=0 || start.Nanosecond()!=0 || start.Minute()%15 != 0 { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"INTERVALO_INVALIDO","message":"El inicio debe coincidir con un intervalo de 15 minutos."}}); return }
	var agenda agendaSemanal
	if err:=s.agendas.FindOne(ctx,bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"diaSemana",Value:int(start.Weekday())},{Key:"activo",Value:true}}).Decode(&agenda); err!=nil { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El profesional no atiende ese día."}}); return }
	from,_:=parseHora(agenda.HoraInicio); to,_:=parseHora(agenda.HoraFin)
	minute:=start.Hour()*60+start.Minute()
	if minute<from || minute+duracionMinutos>to { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El horario queda fuera de la agenda del profesional."}}); return }
	var overlap turno
	err=s.turnos.FindOne(ctx,bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"estado",Value:"confirmado"},{Key:"inicio",Value:bson.D{{Key:"$lt",Value:end}}},{Key:"fin",Value:bson.D{{Key:"$gt",Value:start}}}}).Decode(&overlap)
	if err==nil { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"HORARIO_OCUPADO","message":"Ese horario ya está reservado."}}); return }
	if !errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo verificar el horario"}); return }
	doc:=turno{ID:bson.NewObjectID().Hex(),ClienteID:u.ID,ProfesionalID:req.ProfesionalID,TratamientoID:tratamiento.ID,Inicio:start.UTC(),Fin:end.UTC(),Estado:"confirmado",CreadoEn:time.Now().UTC()}
	// El índice único en profesionalId + minuto coordina todas las réplicas.
	bloqueos := make([]interface{}, 0, duracionMinutos)
	for minuto := start.UTC().Unix()/60; minuto < end.UTC().Unix()/60; minuto++ {
		bloqueos = append(bloqueos, bloqueoTurno{ProfesionalID:req.ProfesionalID, Minuto:minuto, TurnoID:doc.ID})
	}
	if len(bloqueos) > 0 {
		if _, err := s.bloqueos.InsertMany(ctx, bloqueos); err != nil {
			_, _ = s.bloqueos.DeleteMany(context.Background(), bson.D{{Key:"turnoId",Value:doc.ID}})
			if mongo.IsDuplicateKeyError(err) { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"HORARIO_OCUPADO","message":"Ese horario ya está reservado o fue tomado por otra solicitud."}}); return }
			c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo bloquear el horario de forma segura"}); return
		}
	}
	if _,err:=s.turnos.InsertOne(ctx,doc); err!=nil {
		_, _ = s.bloqueos.DeleteMany(context.Background(), bson.D{{Key:"turnoId",Value:doc.ID}})
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo crear el turno"}); return
	}
	c.JSON(http.StatusCreated,gin.H{"turno":doc})
}

func (s *servidorReservas) listarTurnos(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	filter:=bson.D{}
	if tieneRol(u,"recepcion","administrador","superadministrador") {
		// Personal autorizado puede consultar todos los turnos.
	} else if tieneRol(u,"cliente") {
		filter=append(filter,bson.E{Key:"clienteId",Value:u.ID})
	} else if tieneRol(u,"profesional") {
		filter=append(filter,bson.E{Key:"profesionalId",Value:u.ID})
	} else {
		c.JSON(http.StatusForbidden,gin.H{"error":gin.H{"code":"PERMISOS_INSUFICIENTES","message":"No tenés permisos para consultar turnos."}})
		return
	}
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	cur,err:=s.turnos.Find(ctx,filter,options.Find().SetSort(bson.D{{Key:"inicio",Value:1}}))
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudieron consultar los turnos"}); return }
	defer cur.Close(ctx)
	out:=[]turno{}
	if err:=cur.All(ctx,&out); err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudieron leer los turnos"}); return }
	c.JSON(http.StatusOK,gin.H{"turnos":out})
}

func (s *servidorReservas) devolverSena(ctx context.Context, pagoID string, importe float64, idempotencyKey string) error {
	if s.pagosURL=="" { return errors.New("proveedor de pagos no configurado") }
	payload,_:=json.Marshal(gin.H{"pagoId":pagoID,"importe":importe,"moneda":"ARS","motivo":"cancelacion_turno"})
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,s.pagosURL+"/payments/refunds",strings.NewReader(string(payload)))
	if err!=nil { return err }
	req.Header.Set("Content-Type","application/json")
	req.Header.Set("Idempotency-Key",idempotencyKey)
	resp,err:=s.httpClient.Do(req)
	if err!=nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode!=http.StatusOK && resp.StatusCode!=http.StatusCreated && resp.StatusCode!=http.StatusAccepted { return errors.New("el proveedor no confirmó la devolución") }
	return nil
}

func (s *servidorReservas) cancelarTurno(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad); id:=strings.TrimSpace(c.Param("id"))
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	var t turno
	if err:=s.turnos.FindOne(ctx,bson.D{{Key:"_id",Value:id}}).Decode(&t); err!=nil {
		if errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusNotFound,gin.H{"error":"turno no encontrado"}); return }
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo consultar el turno"}); return
	}
	own:=t.ClienteID==u.ID
	staff:=tieneRol(u,"recepcion","administrador","superadministrador")
	if !own && !staff { c.JSON(http.StatusForbidden,gin.H{"error":gin.H{"code":"PERMISOS_INSUFICIENTES","message":"Solo podés cancelar tus propios turnos."}}); return }
	if t.Estado!="confirmado" { c.JSON(http.StatusConflict,gin.H{"error":"el turno ya no está confirmado"}); return }
	ahora:=time.Now().UTC()
	if t.Inicio.Before(ahora) { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"TURNO_INICIADO","message":"No se puede cancelar un turno que ya comenzó."}}); return }
	devolucion:=0.0
	estadoDevolucion:="no_corresponde"
	if t.PagoID!="" { devolucion=importeDevolucion(t.Sena,t.Inicio,ahora); if devolucion>0 { estadoDevolucion="pendiente" } }
	res,err:=s.turnos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"estado",Value:"confirmado"}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"cancelado"},{Key:"importeDevolucion",Value:devolucion},{Key:"estadoDevolucion",Value:estadoDevolucion}}}})
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo cancelar el turno"}); return }
	if res.MatchedCount!=1 { c.JSON(http.StatusConflict,gin.H{"error":"el turno ya no está confirmado"}); return }
	_, _ = s.bloqueos.DeleteMany(ctx, bson.D{{Key:"turnoId",Value:id}})
	if devolucion>0 {
		if err:=s.devolverSena(ctx,t.PagoID,devolucion,"refund-turno-"+id); err!=nil { c.JSON(http.StatusAccepted,gin.H{"turnoId":id,"estado":"cancelado","devolucion":gin.H{"importe":devolucion,"moneda":"ARS","estado":"pendiente","mensaje":"La cancelación quedó registrada; la devolución requiere reintento operativo."}}); return }
		_,_ = s.turnos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"estado",Value:"cancelado"},{Key:"estadoDevolucion",Value:"pendiente"}},bson.D{{Key:"$set",Value:bson.D{{Key:"estadoDevolucion",Value:"devuelta"}}}})
		estadoDevolucion="devuelta"
	}
	c.JSON(http.StatusOK,gin.H{"turnoId":id,"estado":"cancelado","devolucion":gin.H{"importe":devolucion,"moneda":"ARS","estado":estadoDevolucion,"regla":"100% si se cancela con 24 horas o más; sin devolución con menos de 24 horas"}})
}


type bloqueoCarrito struct {
	ID string `bson:"_id" json:"id"`
	ClienteID string `bson:"clienteId" json:"clienteId"`
	ProfesionalID string `bson:"profesionalId" json:"profesionalId"`
	TratamientoID string `bson:"tratamientoId" json:"tratamientoId"`
	Inicio time.Time `bson:"inicio" json:"inicio"`
	Fin time.Time `bson:"fin" json:"fin"`
	Precio float64 `bson:"precio" json:"precio"`
	Moneda string `bson:"moneda" json:"moneda"`
	Sena float64 `bson:"sena" json:"sena"`
	Estado string `bson:"estado" json:"estado"`
	ExpiraEn time.Time `bson:"expiraEn" json:"expiraEn"`
	TurnoID string `bson:"turnoId,omitempty" json:"turnoId,omitempty"`
	PagoID string `bson:"pagoId,omitempty" json:"pagoId,omitempty"`
}

type solicitudCarrito struct {
	ProfesionalID string `json:"profesionalId" binding:"required"`
	TratamientoID string `json:"tratamientoId" binding:"required"`
	Inicio string `json:"inicio" binding:"required"`
}

type confirmarCarritoRequest struct {
	PagoID string `json:"pagoId" binding:"required"`
}

type verificacionPagoRequest struct {
	PagoID string `json:"pagoId"`
	Importe float64 `json:"importe"`
	Moneda string `json:"moneda"`
	Referencia string `json:"referencia"`
}

type verificacionPagoResponse struct {
	Aprobado bool `json:"aprobado"`
	PagoID string `json:"pagoId"`
	Importe float64 `json:"importe"`
	Moneda string `json:"moneda"`
}

func importeSena(precio float64) float64 {
	return math.Round(precio*0.30*100) / 100
}

func importeDevolucion(sena float64, inicio, ahora time.Time) float64 {
	if sena > 0 && !inicio.Before(ahora.Add(24*time.Hour)) { return sena }
	return 0
}

func (s *servidorReservas) verificarPago(ctx context.Context, pagoID string, importe float64, moneda, referencia string) error {
	if s.pagosURL == "" { return errors.New("el proveedor de pagos no está configurado; la reserva no puede confirmarse") }
	payload, _ := json.Marshal(verificacionPagoRequest{PagoID:pagoID, Importe:importe, Moneda:moneda, Referencia:referencia})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.pagosURL+"/payments/verify", strings.NewReader(string(payload)))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", referencia)
	resp, err := s.httpClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return errors.New("el proveedor de pagos no pudo verificar la seña") }
	var result verificacionPagoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { return err }
	if !result.Aprobado || result.PagoID != pagoID || math.Abs(result.Importe-importe)>0.009 || result.Moneda!=moneda { return errors.New("el pago no está aprobado por el importe y moneda exactos de la seña") }
	return nil
}

func (s *servidorReservas) crearBloqueoCarrito(c *gin.Context) {
	u := c.MustGet("identidad").(identidad)
	var req solicitudCarrito
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":gin.H{"code":"CARRITO_INVALIDO","message":"Indicá profesionalId, tratamientoId e inicio RFC3339."}}); return }
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second); defer cancel()
	t, status, err := s.obtenerTratamiento(ctx, req.TratamientoID)
	if err != nil { c.JSON(status, gin.H{"error":gin.H{"code":"TRATAMIENTO_NO_VALIDO","message":err.Error()}}); return }
	if !tratamientoCompatible(t, req.ProfesionalID) { c.JSON(http.StatusUnprocessableEntity, gin.H{"error":gin.H{"code":"PROFESIONAL_NO_APTO","message":"El profesional no está habilitado para este tratamiento."}}); return }
	if t.Precio < 0 || (t.Moneda != "" && t.Moneda != "ARS") { c.JSON(http.StatusUnprocessableEntity, gin.H{"error":gin.H{"code":"PRECIO_INVALIDO","message":"El tratamiento no tiene un precio válido en ARS."}}); return }
	start, err := time.Parse(time.RFC3339, req.Inicio)
	if err != nil || !start.After(time.Now()) || start.Second()!=0 || start.Nanosecond()!=0 || start.Minute()%15!=0 { c.JSON(http.StatusBadRequest, gin.H{"error":gin.H{"code":"INTERVALO_INVALIDO","message":"El inicio debe ser futuro, RFC3339 y coincidir con un intervalo de 15 minutos."}}); return }
	end := start.Add(time.Duration(t.DuracionMinutos)*time.Minute)
	var agenda agendaSemanal
	if err := s.agendas.FindOne(ctx, bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"diaSemana",Value:int(start.Weekday())},{Key:"activo",Value:true}}).Decode(&agenda); err != nil { c.JSON(http.StatusConflict, gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El profesional no atiende ese día."}}); return }
	from,_ := parseHora(agenda.HoraInicio); to,_ := parseHora(agenda.HoraFin)
	if start.Hour()*60+start.Minute()<from || start.Hour()*60+start.Minute()+t.DuracionMinutos>to { c.JSON(http.StatusConflict, gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El horario queda fuera de la agenda del profesional."}}); return }
	now := time.Now().UTC()
	_, _ = s.bloqueos.DeleteMany(ctx, bson.D{{Key:"expiraEn",Value:bson.D{{Key:"$lte",Value:now}}}})
	var overlap turno
	err = s.turnos.FindOne(ctx,bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"estado",Value:"confirmado"},{Key:"inicio",Value:bson.D{{Key:"$lt",Value:end}}},{Key:"fin",Value:bson.D{{Key:"$gt",Value:start}}}}).Decode(&overlap)
	if err == nil { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"HORARIO_OCUPADO","message":"Ese horario ya está reservado."}}); return }
	if !errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo verificar el horario"}); return }
	id := bson.NewObjectID().Hex()
	expira := now.Add(10*time.Minute)
	doc := bloqueoCarrito{ID:id,ClienteID:u.ID,ProfesionalID:req.ProfesionalID,TratamientoID:t.ID,Inicio:start.UTC(),Fin:end.UTC(),Precio:t.Precio,Moneda:"ARS",Sena:importeSena(t.Precio),Estado:"pendiente_pago",ExpiraEn:expira}
	bloqueos := make([]interface{},0,t.DuracionMinutos)
	for minuto:=start.UTC().Unix()/60; minuto<end.UTC().Unix()/60; minuto++ { bloqueos=append(bloqueos,bloqueoTurno{ProfesionalID:req.ProfesionalID,Minuto:minuto,TurnoID:id,ExpiraEn:&expira}) }
	if _,err:=s.bloqueos.InsertMany(ctx,bloqueos); err!=nil {
		_,_ = s.bloqueos.DeleteMany(context.Background(),bson.D{{Key:"turnoId",Value:id}})
		if mongo.IsDuplicateKeyError(err) { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"HORARIO_OCUPADO","message":"Ese horario ya está reservado o bloqueado por otro carrito."}}); return }
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo bloquear el horario"}); return
	}
	if _,err:=s.carritos.InsertOne(ctx,doc); err!=nil { _,_ = s.bloqueos.DeleteMany(context.Background(),bson.D{{Key:"turnoId",Value:id}}); c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo guardar el carrito"}); return }
	c.JSON(http.StatusCreated,gin.H{"bloqueo":doc,"seña":gin.H{"porcentaje":30,"importe":doc.Sena,"moneda":doc.Moneda},"mensaje":"El horario queda bloqueado por 10 minutos. Para confirmar, aboná la seña y enviá el pago aprobado."})
}

func (s *servidorReservas) liberarBloqueoCarrito(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	id:=strings.TrimSpace(c.Param("id"))
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	res,err:=s.carritos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"clienteId",Value:u.ID},{Key:"estado",Value:"pendiente_pago"}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"liberado"},{Key:"expiraEn",Value:time.Now().UTC()}}}})
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo liberar el carrito"}); return }
	if res.MatchedCount==0 { c.JSON(http.StatusNotFound,gin.H{"error":gin.H{"code":"CARRITO_NO_ENCONTRADO","message":"No existe un bloqueo pendiente de tu propiedad."}}); return }
	_,_ = s.bloqueos.DeleteMany(ctx,bson.D{{Key:"turnoId",Value:id}})
	c.Status(http.StatusNoContent)
}

func (s *servidorReservas) confirmarBloqueoCarrito(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	id:=strings.TrimSpace(c.Param("id"))
	var req confirmarCarritoRequest
	if err:=c.ShouldBindJSON(&req); err!=nil || strings.TrimSpace(req.PagoID)=="" { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"PAGO_INVALIDO","message":"Indicá pagoId del proveedor de pagos."}}); return }
	ctx,cancel:=context.WithTimeout(c.Request.Context(),8*time.Second); defer cancel()
	var cart bloqueoCarrito
	if err:=s.carritos.FindOne(ctx,bson.D{{Key:"_id",Value:id}}).Decode(&cart); err!=nil {
		if errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusGone,gin.H{"error":gin.H{"code":"BLOQUEO_VENCIDO","message":"El bloqueo venció o no existe. Volvé a elegir el horario."}}); return }
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo consultar el carrito"}); return
	}
	if cart.ClienteID!=u.ID { c.JSON(http.StatusForbidden,gin.H{"error":"el carrito pertenece a otro cliente"}); return }
	if cart.Estado=="confirmado" && cart.TurnoID!="" { var existente turno; if err:=s.turnos.FindOne(ctx,bson.D{{Key:"_id",Value:cart.TurnoID}}).Decode(&existente); err==nil { c.JSON(http.StatusOK,gin.H{"turno":existente,"mensaje":"La reserva ya había sido confirmada; no se creó un duplicado."}); return } }
	if cart.Estado!="pendiente_pago" || !cart.ExpiraEn.After(time.Now()) { c.JSON(http.StatusGone,gin.H{"error":gin.H{"code":"BLOQUEO_VENCIDO","message":"El bloqueo venció; iniciá una nueva reserva."}}); return }
	if err:=s.verificarPago(ctx,req.PagoID,cart.Sena,cart.Moneda,id); err!=nil { c.JSON(http.StatusPaymentRequired,gin.H{"error":gin.H{"code":"SENA_NO_APROBADA","message":err.Error()}}); return }
	// La reserva sólo se confirma si el proveedor valida el pago por el importe exacto.
	now:=time.Now().UTC()
	doc:=turno{ID:bson.NewObjectID().Hex(),ClienteID:cart.ClienteID,ProfesionalID:cart.ProfesionalID,TratamientoID:cart.TratamientoID,Inicio:cart.Inicio,Fin:cart.Fin,Estado:"confirmado",CreadoEn:now,PrecioTratamiento:cart.Precio,Sena:cart.Sena,PagoID:req.PagoID,EstadoPago:"aprobado"}
	res,err:=s.carritos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"estado",Value:"pendiente_pago"},{Key:"expiraEn",Value:bson.D{{Key:"$gt",Value:now}}}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"confirmando"},{Key:"pagoId",Value:req.PagoID}}}})
	if err!=nil || res.MatchedCount!=1 { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"CARRITO_PROCESADO","message":"El carrito venció o ya está siendo confirmado."}}); return }
	if _,err=s.turnos.InsertOne(ctx,doc); err!=nil {
		_,_ = s.carritos.UpdateOne(context.Background(),bson.D{{Key:"_id",Value:id}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"pendiente_pago"}}},{Key:"$unset",Value:bson.D{{Key:"pagoId",Value:""}}}})
		c.JSON(http.StatusServiceUnavailable,gin.H{"error":"se verificó el pago pero no se pudo confirmar el turno; contactá soporte con el identificador del pago"}); return
	}
	_,err=s.bloqueos.UpdateMany(ctx,bson.D{{Key:"turnoId",Value:id}},bson.D{{Key:"$set",Value:bson.D{{Key:"turnoId",Value:doc.ID}}},{Key:"$unset",Value:bson.D{{Key:"expiraEn",Value:""}}}})
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"turno creado pero no se pudo consolidar el bloqueo; requiere recuperación operativa"}); return }
	_,_ = s.carritos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"confirmado"},{Key:"turnoId",Value:doc.ID}}}})
	c.JSON(http.StatusCreated,gin.H{"turno":doc,"pago":gin.H{"estado":"aprobado","importe":cart.Sena,"moneda":cart.Moneda}})
}

func (s *servidorReservas) confirmarTurnoDesdeCarrito(c *gin.Context) {
	c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"CARRITO_REQUERIDO","message":"Primero bloqueá el horario con POST /carrito/bloqueos y luego confirmalo con el pago aprobado en POST /carrito/bloqueos/{id}/confirmar."}})
}

func (s *servidorReservas) reintentarDevolucion(c *gin.Context) {
	u:=c.MustGet("identidad").(identidad)
	id:=strings.TrimSpace(c.Param("id"))
	ctx,cancel:=context.WithTimeout(c.Request.Context(),8*time.Second); defer cancel()
	var t turno
	if err:=s.turnos.FindOne(ctx,bson.D{{Key:"_id",Value:id}}).Decode(&t); err!=nil { c.JSON(http.StatusNotFound,gin.H{"error":"turno no encontrado"}); return }
	if t.ClienteID!=u.ID && !tieneRol(u,"recepcion","administrador","superadministrador") { c.JSON(http.StatusForbidden,gin.H{"error":"no tenés permiso para gestionar esta devolución"}); return }
	if t.Estado!="cancelado" || t.EstadoDevolucion!="pendiente" || t.ImporteDevolucion<=0 || t.PagoID=="" { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"DEVOLUCION_NO_PENDIENTE","message":"El turno no tiene una devolución pendiente."}}); return }
	if err:=s.devolverSena(ctx,t.PagoID,t.ImporteDevolucion,"refund-turno-"+id); err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":gin.H{"code":"DEVOLUCION_PENDIENTE","message":"El proveedor todavía no confirmó la devolución; se puede reintentar."}}); return }
	_,err:=s.turnos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"estadoDevolucion",Value:"pendiente"}},bson.D{{Key:"$set",Value:bson.D{{Key:"estadoDevolucion",Value:"devuelta"}}}})
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"la devolución fue enviada pero no se pudo actualizar su estado"}); return }
	c.JSON(http.StatusOK,gin.H{"turnoId":id,"devolucion":gin.H{"importe":t.ImporteDevolucion,"moneda":"ARS","estado":"devuelta"}})
}

func slotNoSuperpuesto(start,end,otherStart,otherEnd time.Time) bool { return !start.Before(otherEnd) || !end.After(otherStart) }
func fmtSscanf(s, format string, n *int) (int,error) { return fmt.Sscanf(s,format,n) }


package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
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
}

type servidorReservas struct {
	agendas *mongo.Collection
	turnos *mongo.Collection
	mu sync.Mutex
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
	s := &servidorReservas{agendas: db.Collection("agendas_semanales"), turnos: db.Collection("turnos")}
	_, err = s.agendas.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"profesionalId",Value:1},{Key:"diaSemana",Value:1}}, Options: options.Index().SetUnique(true).SetName("uq_agenda_profesional_dia")})
	if err != nil { log.Fatal("no se pudo crear índice de agenda") }
	_, err = s.turnos.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key:"profesionalId",Value:1},{Key:"inicio",Value:1}}, Options: options.Index().SetName("ix_turnos_profesional_inicio")})
	if err != nil { log.Fatal("no se pudo crear índice de turnos") }
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
	r.POST("/turnos", requiereIdentidad("cliente"), requierePermiso("reservas.crear"), s.crearTurno)
	r.DELETE("/turnos/:id", requiereIdentidad(), requierePermiso("reservas.cancelar-turno"), s.cancelarTurno)
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
	prof:=strings.TrimSpace(c.Query("profesionalId")); fecha,err:=time.Parse("2006-01-02",c.Query("fecha")); dur,errDur:=parsePositiveInt(c.Query("duracionMinutos"))
	if prof=="" || err!=nil || errDur!=nil || dur<5 || dur>480 { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"PARAMETROS_INVALIDOS","message":"Usá profesionalId, fecha (YYYY-MM-DD) y duracionMinutos entre 5 y 480."}}); return }
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
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
	var req struct { ProfesionalID string `json:"profesionalId" binding:"required"`; TratamientoID string `json:"tratamientoId" binding:"required"`; Inicio string `json:"inicio" binding:"required"`; DuracionMinutos int `json:"duracionMinutos" binding:"required"` }
	if err:=c.ShouldBindJSON(&req); err!=nil || req.DuracionMinutos<5 || req.DuracionMinutos>480 { c.JSON(http.StatusBadRequest,gin.H{"error":gin.H{"code":"TURNO_INVALIDO","message":"Indicá profesional, tratamiento, inicio RFC3339 y duración entre 5 y 480 minutos."}}); return }
	start,err:=time.Parse(time.RFC3339,req.Inicio); if err!=nil || !start.After(time.Now()) { c.JSON(http.StatusBadRequest,gin.H{"error":"el inicio debe ser una fecha futura RFC3339"}); return }
	end:=start.Add(time.Duration(req.DuracionMinutos)*time.Minute)
	s.mu.Lock(); defer s.mu.Unlock()
	ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second); defer cancel()
	var agenda agendaSemanal
	if err:=s.agendas.FindOne(ctx,bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"diaSemana",Value:int(start.Weekday())},{Key:"activo",Value:true}}).Decode(&agenda); err!=nil { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El profesional no atiende ese día."}}); return }
	from,_:=parseHora(agenda.HoraInicio); to,_:=parseHora(agenda.HoraFin)
	minute:=start.Hour()*60+start.Minute()
	if minute<from || minute+req.DuracionMinutos>to || start.Second()!=0 || start.Nanosecond()!=0 { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"FUERA_DE_AGENDA","message":"El horario queda fuera de la agenda del profesional."}}); return }
	var overlap turno
	err=s.turnos.FindOne(ctx,bson.D{{Key:"profesionalId",Value:req.ProfesionalID},{Key:"estado",Value:"confirmado"},{Key:"inicio",Value:bson.D{{Key:"$lt",Value:end}}},{Key:"fin",Value:bson.D{{Key:"$gt",Value:start}}}}).Decode(&overlap)
	if err==nil { c.JSON(http.StatusConflict,gin.H{"error":gin.H{"code":"HORARIO_OCUPADO","message":"Ese horario ya está reservado."}}); return }
	if !errors.Is(err,mongo.ErrNoDocuments) { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo verificar el horario"}); return }
	doc:=turno{ID:bson.NewObjectID().Hex(),ClienteID:u.ID,ProfesionalID:req.ProfesionalID,TratamientoID:req.TratamientoID,Inicio:start.UTC(),Fin:end.UTC(),Estado:"confirmado",CreadoEn:time.Now().UTC()}
	if _,err:=s.turnos.InsertOne(ctx,doc); err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo crear el turno"}); return }
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
	_,err:=s.turnos.UpdateOne(ctx,bson.D{{Key:"_id",Value:id},{Key:"estado",Value:"confirmado"}},bson.D{{Key:"$set",Value:bson.D{{Key:"estado",Value:"cancelado"}}}})
	if err!=nil { c.JSON(http.StatusServiceUnavailable,gin.H{"error":"no se pudo cancelar el turno"}); return }
	c.Status(http.StatusNoContent)
}

func slotNoSuperpuesto(start,end,otherStart,otherEnd time.Time) bool { return !start.Before(otherEnd) || !end.After(otherStart) }
func fmtSscanf(s, format string, n *int) (int,error) { return fmt.Sscanf(s,format,n) }


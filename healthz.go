package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	time "time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var logger = new(logrus.Logger)

func SetLogger(l *logrus.Logger) {
	logger = l
}

type Component struct {
	Type   string    `json:"type"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	Check  string    `json:"check"`
	Time   time.Time `json:"time"`
}
type Healthz struct {
	Status   string       `json:"status"`
	Metadata MapMetadata  `json:"metadata"`
	Notes    []string     `json:"notes"`
	Details  MapComponent `json:"details"`
	checks   MapCheckFunc
	config   HealthzConfig
}

func (h *Healthz) Set(k, v string) {
	if h.Metadata == nil {
		h.Metadata = make(MapMetadata)
	}
	h.Metadata[k] = v
}
func (h *Healthz) Get(k string) string {
	if h.Metadata == nil {
		return ""
	} else if v, ok := h.Metadata[k]; !ok {
		return ""
	} else {
		return v
	}

}

func (h *Healthz) Handler() func(*gin.Context) {
	return func(c *gin.Context) {
		HealthzHandler(h, c)
	}
}

type Status int
type CheckFunc func() Status
type HealthMonitorFunc func(h *Healthz, d *Component) Status
type MapCheckFunc map[string]CheckFunc
type MapComponent map[string]*Component
type MapMetadata map[string]string

func CheckFuncHelper(f HealthMonitorFunc, h *Healthz, d *Component) CheckFunc {

	return func() (s Status) {
		s = StatusFail
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Recovered from a panic caused by HealthMonitor %s", d.Name)
				h.Note("Recovered from a panic caused by HealthMonitor", d.Name)
				d.Status = s.String()
				d.Time = time.Now()
			}
		}()

		s = f(h, d)
		d.Status = s.String()
		return s
	}
}
func (h *Healthz) Note(s ...string) {
	h.Notes = append(h.Notes, strings.Join(s, " "))
}
func (h *Healthz) AddCheck(t, s string, f HealthMonitorFunc) {
	if h.checks == nil {
		h.checks = make(MapCheckFunc)
	}
	if h.Details == nil {
		h.Details = make(MapComponent)
	}
	h.Details[s] = &Component{Type: t, Name: s}
	h.checks[s] = CheckFuncHelper(f, h, h.Details[s])
	logger.Infof("%s check has been added to HealthMonitor", s)
}

func (s Status) String() string {
	if s == StatusPass {
		return "pass"
	} else if s == StatusFail {
		return "fail"
	} else if s == StatusWarning {
		return "warn"
	} else {
		return "unknown"
	}
}

const (
	StatusPass Status = iota
	StatusFail
	StatusWarning
)

func randomUUID(prefix string) string {
	uid := strings.ReplaceAll(uuid.New().String(), "-", "")
	return fmt.Sprint(prefix, uid)

}
func PersistentServiceID(file string) string {
	var service string
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		logger.Panicf("Healthz unable to open file %s, %s", file, err)
	}
	b, _ := ioutil.ReadAll(f)
	if len(b) == 0 {
		service = randomUUID(fmt.Sprint("api", strings.ReplaceAll(uuid.New().String(), "-", "")))
		_, err = f.Write([]byte(service))
		if err != nil {
			logger.Panicf("Healthz unable to write to file %s, %s", file, err)
		}
		logger.Infof("New persistent service-id %s has been generated for HealthMonitor", service)
	} else if len(b) > 0 {
		service = string(b)
	} else {
		logger.Panicf("Healthz unable to read file %s, %s", file, err)
	}

	logger.Infof("Use persistent service-id %s for HealthMonitor", service)
	return service
}
func NewHealthZ(c HealthzConfig) *Healthz {
	h := &Healthz{}
	h.config = c
	h.Set("service_id", PersistentServiceID(c.ServiceFile))
	return h
}

type HealthzConfig struct {
	Version     string
	Release     string
	Description string
	ServiceFile string
	NotesCount  int
}

func HealthzHandler(h *Healthz, c *gin.Context) {
	status := StatusPass

	var httpStatus = http.StatusOK
	for c, check := range h.checks {
		s := check()
		h.Note(fmt.Sprintf("%s %s", c, s))

		if (s == StatusFail) || (s == StatusWarning && status != StatusFail) {
			status = s
		}
	}

	if status != StatusPass {
		httpStatus = http.StatusConflict
	}
	if len(h.Notes) > h.config.NotesCount {
		h.Notes = h.Notes[:h.config.NotesCount]
	}

	h.Status = status.String()
	c.JSON(httpStatus, h)

}

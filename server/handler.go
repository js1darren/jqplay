package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/jingweno/jqplay/config"
	"github.com/jingweno/jqplay/jq"
	"golang.org/x/net/context"
	"gopkg.in/gin-gonic/gin.v1"
)

const (
	JSONPayloadLimit   = JSONPayloadLimitMB * OneMB
	JSONPayloadLimitMB = 10
	OneMB              = 1024000
	JQExecTimeout      = 15 * time.Second
)

type JQHandlerContext struct {
	*config.Config
	JQ string
}

func (c *JQHandlerContext) Asset(path string) string {
	return fmt.Sprintf("%s/%s", c.AssetHost, path)
}

func (c *JQHandlerContext) ShouldInitJQ() bool {
	return c.JQ != ""
}

type JQHandler struct {
	DB     *DB
	Config *config.Config
}

func (h *JQHandler) handleIndex(c *gin.Context) {
	c.HTML(200, "index.tmpl", &JQHandlerContext{Config: h.Config})
}

func (h *JQHandler) checkReqSize(c *gin.Context) error {
	if c.Request.ContentLength > JSONPayloadLimit {
		size := float64(c.Request.ContentLength) / OneMB
		return fmt.Errorf("JSON payload size is %.1fMB, larger than limit %dMB.", size, JSONPayloadLimitMB)
	}

	return nil
}

func (h *JQHandler) handleJqPost(c *gin.Context) {
	if err := h.checkReqSize(c); err != nil {
		h.logger(c).WithError(err)
		c.String(http.StatusExpectationFailed, err.Error())
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, JSONPayloadLimit)

	var jq *jq.JQ
	err := c.BindJSON(&jq)
	if err != nil {
		err = fmt.Errorf("error parsing JSON: %s", err)
		h.logger(c).WithError(err).Infof("error parsing JSON")
		c.String(http.StatusUnprocessableEntity, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), JQExecTimeout)
	defer cancel()

	// Evaling into ResponseWriter sets the status code to 200
	// appending error message in the end if there's any
	err = jq.Eval(ctx, c.Writer)
	if err != nil {
		h.logger(c).WithError(err).WithField("j", jq.J).WithField("q", jq.Q).Infof("jq error")
		fmt.Fprint(c.Writer, err.Error())
	}
}

func (h *JQHandler) handleJqGet(c *gin.Context) {
	jq := &jq.JQ{
		J: c.Query("j"),
		Q: c.Query("q"),
	}

	var jqData string
	if err := jq.Validate(); err == nil {
		d, err := json.Marshal(jq)
		if err == nil {
			jqData = string(d)
		}
	}

	c.HTML(http.StatusOK, "index.tmpl", &JQHandlerContext{Config: h.Config, JQ: jqData})
}

func (h *JQHandler) handleJqSharePost(c *gin.Context) {
	if err := h.checkReqSize(c); err != nil {
		h.logger(c).WithError(err)
		c.String(http.StatusExpectationFailed, err.Error())
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, JSONPayloadLimit)

	var jq *jq.JQ
	if err := c.BindJSON(&jq); err != nil {
		err = fmt.Errorf("error parsing JSON: %s", err)
		h.logger(c).WithError(err)
		c.String(http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := jq.Validate(); err != nil {
		h.logger(c).WithError(err)
		c.String(http.StatusUnprocessableEntity, err.Error())
		return
	}

	id, err := h.DB.UpsertSnippet(FromJQ(jq))
	if err != nil {
		h.logger(c).WithError(err)
		c.String(http.StatusUnprocessableEntity, "error sharing snippet")
		return
	}

	c.String(http.StatusCreated, id)
}

func (h *JQHandler) handleJqShareGet(c *gin.Context) {
	id := c.Param("id")

	s, err := h.DB.GetSnippet(id)
	if err != nil {
		h.logger(c).WithError(err).Info("error getting snippet")
		c.Redirect(http.StatusFound, "/")
		return
	}

	var jqData string
	d, err := json.Marshal(ToJQ(s))
	if err == nil {
		jqData = string(d)
	}

	c.HTML(200, "index.tmpl", &JQHandlerContext{
		Config: h.Config,
		JQ:     jqData,
	})
}

func (h *JQHandler) logger(c *gin.Context) *logrus.Entry {
	l, _ := c.Get("logger")
	return l.(*logrus.Entry)
}

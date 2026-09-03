package controller

import (
	"net/http"
	"strings"
	"x-ui/web/service"

	"github.com/gin-gonic/gin"
)

// SubscriptionController exposes only opaque per-user profile URLs. It is
// deliberately outside the authenticated /xui group so Clash Verge can fetch
// updates on its own schedule.
type SubscriptionController struct {
	inboundService service.InboundService
}

func NewSubscriptionController(g *gin.RouterGroup) *SubscriptionController {
	a := &SubscriptionController{}
	g.GET("/sub/:token", a.getVLESSSubscription)
	return a
}

func (a *SubscriptionController) getVLESSSubscription(c *gin.Context) {
	profile, err := a.inboundService.GetVLESSUserSubscription(c.Param("token"), c.Request.Host)
	if err != nil {
		c.String(http.StatusNotFound, "订阅不存在或已失效")
		return
	}
	fileName := strings.ReplaceAll(profile.FileName, "\"", "")
	c.Header("Content-Disposition", "inline; filename=\""+fileName+"\"")
	c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(profile.ClashVergeConfig))
}

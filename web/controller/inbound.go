package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/url"
	"strconv"
	"strings"
	"x-ui/database/model"
	"x-ui/logger"
	"x-ui/web/global"
	"x-ui/web/service"
	"x-ui/web/session"
)

type InboundController struct {
	inboundService  service.InboundService
	xrayService     service.XrayService
	firewallService service.FirewallService
}

type inboundUserProfileForm struct {
	ClientID string `json:"clientId" form:"clientId"`
	Host     string `json:"host" form:"host"`
	Name     string `json:"name" form:"name"`
}

type realityKeyForm struct {
	PrivateKey string `json:"privateKey" form:"privateKey"`
}

type inboundUserSubscription struct {
	URL string `json:"url"`
}

func NewInboundController(g *gin.RouterGroup) *InboundController {
	a := &InboundController{}
	a.initRouter(g)
	a.startTask()
	return a
}

func (a *InboundController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/inbound")

	g.POST("/list", a.getInbounds)
	g.POST("/firewall/:port/status", a.getFirewallPortStatus)
	g.POST("/firewall/:port/open", a.openFirewallPort)
	g.POST("/add", a.addInbound)
	g.POST("/del/:id", a.delInbound)
	g.POST("/update/:id", a.updateInbound)
	g.POST("/:id/userProfile", a.getUserProfile)
	g.POST("/:id/userSubscription", a.getUserSubscription)
	g.POST("/:id/userTraffic", a.getUserTraffic)
	g.POST("/realityKey", a.getRealityKey)
	g.POST("/realityShortID", a.getRealityShortID)
}

func (a *InboundController) getRealityKey(c *gin.Context) {
	form := &realityKeyForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if form.PrivateKey == "" {
		pair, err := service.GenerateRealityKeyPair()
		jsonObj(c, pair, err)
		return
	}
	pair, err := service.GetRealityPublicKey(form.PrivateKey)
	jsonObj(c, pair, err)
}

func (a *InboundController) getRealityShortID(c *gin.Context) {
	shortID, err := service.GenerateRealityShortID()
	jsonObj(c, shortID, err)
}

func (a *InboundController) getFirewallPortStatus(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("端口格式不正确"))
		return
	}
	status, err := a.firewallService.CheckTCPPort(port)
	jsonObj(c, status, err)
}

func (a *InboundController) openFirewallPort(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("端口格式不正确"))
		return
	}
	status, err := a.firewallService.OpenTCPPort(port)
	jsonObj(c, status, err)
}

func (a *InboundController) startTask() {
	webServer := global.GetWebServer()
	c := webServer.GetCron()
	c.AddFunc("@every 10s", func() {
		if a.xrayService.IsNeedRestartAndSetFalse() {
			err := a.xrayService.RestartXray(false)
			if err != nil {
				logger.Error("restart xray failed:", err)
			}
		}
	})
}

func (a *InboundController) getInbounds(c *gin.Context) {
	user := session.GetLoginUser(c)
	inbounds, err := a.inboundService.GetInbounds(user.Id)
	if err != nil {
		jsonMsg(c, "获取", err)
		return
	}
	jsonObj(c, inbounds, nil)
}

func (a *InboundController) addInbound(c *gin.Context) {
	inbound := &model.Inbound{}
	err := c.ShouldBind(inbound)
	if err != nil {
		jsonMsg(c, "添加", err)
		return
	}
	user := session.GetLoginUser(c)
	if inbound.Protocol != model.VLESS {
		jsonMsg(c, "添加", fmt.Errorf("仅支持 VLESS 入站"))
		return
	}
	inbound.UserId = user.Id
	inbound.Enable = true
	inbound.Tag = fmt.Sprintf("inbound-%v", inbound.Port)
	err = a.inboundService.AddInbound(inbound)
	jsonMsg(c, "添加", err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *InboundController) delInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "删除", err)
		return
	}
	err = a.inboundService.DelInbound(id)
	jsonMsg(c, "删除", err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *InboundController) updateInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "修改", err)
		return
	}
	inbound := &model.Inbound{
		Id: id,
	}
	err = c.ShouldBind(inbound)
	if err != nil {
		jsonMsg(c, "修改", err)
		return
	}
	if inbound.Protocol != model.VLESS {
		jsonMsg(c, "修改", fmt.Errorf("仅支持 VLESS 入站"))
		return
	}
	err = a.inboundService.UpdateInbound(inbound)
	jsonMsg(c, "修改", err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *InboundController) getUserProfile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "获取用户配置", err)
		return
	}
	form := &inboundUserProfileForm{}
	if err = c.ShouldBind(form); err != nil {
		jsonMsg(c, "获取用户配置", err)
		return
	}
	user := session.GetLoginUser(c)
	profile, err := a.inboundService.GetVLESSUserProfile(id, user.Id, form.ClientID, form.Host, form.Name)
	jsonObj(c, profile, err)
}

func (a *InboundController) getUserSubscription(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("入站编号格式不正确"))
		return
	}
	form := &inboundUserProfileForm{}
	if err = c.ShouldBind(form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	user := session.GetLoginUser(c)
	token, err := a.inboundService.GetOrCreateVLESSUserSubscriptionToken(id, user.Id, form.ClientID)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	jsonObj(c, &inboundUserSubscription{URL: subscriptionURL(c, token)}, nil)
}

func subscriptionURL(c *gin.Context, token string) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + c.GetString("base_path") + "sub/" + url.PathEscape(token)
}

func (a *InboundController) getUserTraffic(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("入站编号格式不正确"))
		return
	}
	user := session.GetLoginUser(c)
	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	if inbound.UserId != user.Id {
		jsonObj(c, nil, fmt.Errorf("无权访问该入站"))
		return
	}
	traffics, err := a.inboundService.GetInboundUserTraffic(id)
	jsonObj(c, traffics, err)
}

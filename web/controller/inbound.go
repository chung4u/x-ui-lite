package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/url"
	"strconv"
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

type realityCandidateForm struct {
	Candidate string `json:"candidate" form:"candidate"`
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

	g.POST("/:id/hysteria", func(c *gin.Context) {
		id, err := a.policyInboundID(c)
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		var form struct {
			Client string `json:"clientId" form:"clientId"`
			Action string `json:"action" form:"action"`
		}
		if err = c.ShouldBind(&form); err != nil {
			jsonObj(c, nil, err)
			return
		}
		obj, err := service.HyManage(id, form.Client, form.Action)
		jsonObj(c, obj, err)
	})
	g.POST("/list", a.getInbounds)
	g.POST("/archived", a.getArchivedInbounds)
	g.POST("/runtime", a.getRuntimeState)
	g.POST("/:id/userPolicies", a.getUserPolicies)
	g.POST("/:id/endpointStatus", a.getEndpointStatus)
	g.POST("/:id/userPolicies/update", a.updateUserPolicy)
	g.POST("/:id/user/save", a.saveManagedUser)
	g.POST("/:id/quotaGroup/update", a.updateQuotaGroup)
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
	g.POST("/realityCandidate", a.checkRealityCandidate)
	g.POST("/realityUUID", a.getRealityUUID)
}

func (a *InboundController) getRealityUUID(c *gin.Context) {
	identifier, err := service.GenerateInboundUserUUID()
	jsonObj(c, identifier, err)
}

func (a *InboundController) checkRealityCandidate(c *gin.Context) {
	form := &realityCandidateForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	result, err := service.CheckRealityCandidate(c.Request.Context(), form.Candidate)
	jsonObj(c, result, err)
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
	inbound.Id = 0
	inbound.ArchivedAt = 0
	inbound.Up = 0
	inbound.Down = 0
	inbound.ResetTraffic = false
	inbound.UserId = user.Id
	inbound.Enable = true
	inbound.Tag = fmt.Sprintf("inbound-%v", inbound.Port)
	err = a.inboundService.AddInbound(inbound)
	if err != nil {
		jsonMsg(c, "添加", err)
		return
	}
	jsonMsgObj(c, "添加", gin.H{"id": inbound.Id}, nil)
	a.xrayService.SetToNeedRestart()
}

func (a *InboundController) delInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "删除", err)
		return
	}
	if _, err = a.ownedInbound(c, id, false); err != nil {
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
	inbound.Id = id
	if _, err = a.ownedInbound(c, id, false); err != nil {
		jsonMsg(c, "修改", err)
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
	return session.Origin(c) + c.GetString("base_path") + "sub/" + url.PathEscape(token)
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

func (a *InboundController) ownedInbound(c *gin.Context, id int, allowArchived bool) (*model.Inbound, error) {
	inbound, err := a.inboundService.GetInbound(id)
	if err != nil {
		return nil, err
	}
	user := session.GetLoginUser(c)
	if inbound.UserId != user.Id {
		return nil, fmt.Errorf("无权访问该入站")
	}
	if !allowArchived && inbound.ArchivedAt != 0 {
		return nil, fmt.Errorf("归档入口只读")
	}
	return inbound, nil
}
func (a *InboundController) getArchivedInbounds(c *gin.Context) {
	user := session.GetLoginUser(c)
	rows, err := a.inboundService.GetArchivedInbounds(user.Id)
	jsonObj(c, rows, err)
}
func (a *InboundController) policyInboundID(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return 0, fmt.Errorf("入站编号格式不正确")
	}
	_, err = a.ownedInbound(c, id, false)
	return id, err
}
func (a *InboundController) getUserPolicies(c *gin.Context) {
	id, err := a.policyInboundID(c)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	view, err := a.inboundService.GetUserPolicies(id)
	jsonObj(c, view, err)
}
func (a *InboundController) updateUserPolicy(c *gin.Context) {
	id, err := a.policyInboundID(c)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	form := struct {
		ClientID string `json:"clientId" form:"clientId"`
		Enabled  *bool  `json:"enabled" form:"enabled"`
		GroupID  int    `json:"groupId" form:"groupId"`
	}{}
	if err = c.ShouldBind(&form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if form.Enabled == nil {
		jsonObj(c, nil, fmt.Errorf("缺少用户启用状态"))
		return
	}
	if err = a.xrayService.CollectTraffic(); err != nil {
		jsonObj(c, nil, fmt.Errorf("保存前流量采集失败"))
		return
	}
	err = a.inboundService.UpdateUserPolicy(id, form.ClientID, *form.Enabled, form.GroupID)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, "保存用户策略", err)
}
func (a *InboundController) updateQuotaGroup(c *gin.Context) {
	id, err := a.policyInboundID(c)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	form := struct {
		Id         int    `json:"id" form:"id"`
		Name       string `json:"name" form:"name"`
		Total      int64  `json:"total" form:"total"`
		ExpiryTime int64  `json:"expiryTime" form:"expiryTime"`
		Enabled    *bool  `json:"enabled" form:"enabled"`
	}{}
	if err = c.ShouldBind(&form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if form.Enabled == nil {
		jsonObj(c, nil, fmt.Errorf("缺少额度组启用状态"))
		return
	}
	saved, err := a.inboundService.UpdateQuotaGroup(id, &model.InboundQuotaGroup{Id: form.Id, Name: form.Name, Total: form.Total, ExpiryTime: form.ExpiryTime, Enabled: *form.Enabled})
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
	jsonObj(c, saved, err)
}

func (a *InboundController) getRuntimeState(c *gin.Context) {
	jsonObj(c, a.xrayService.GetApplyState(), nil)
}

func (a *InboundController) saveManagedUser(c *gin.Context) {
	id, err := a.policyInboundID(c)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	var form service.ManagedUserChange
	if err = c.ShouldBind(&form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if err = a.xrayService.CollectTraffic(); err != nil {
		jsonObj(c, nil, fmt.Errorf("保存前流量采集失败，请重试"))
		return
	}
	err = a.inboundService.SaveManagedUser(session.GetLoginUser(c).Id, id, &form)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, "保存用户", err)
}

func (a *InboundController) getEndpointStatus(c *gin.Context) {
	id, err := a.policyInboundID(c)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	status, err := a.inboundService.GetEndpointStatus(id)
	jsonObj(c, status, err)
}

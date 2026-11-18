package controller

import (
	"github.com/gin-gonic/gin"
	"strconv"
	"sync"
	"time"
	"x-ui/logger"
	"x-ui/web/global"
	"x-ui/web/service"
	"x-ui/web/session"
)

type ServerController struct {
	BaseController

	serverService service.ServerService

	sampleMu          sync.Mutex
	statusMu          sync.Mutex
	lastStatus        *service.Status
	lastGetStatusTime time.Time

	lastVersions        []string
	lastGetVersionsTime time.Time
}

func NewServerController(g *gin.RouterGroup) *ServerController {
	a := &ServerController{
		lastGetStatusTime: time.Now(),
	}
	a.initRouter(g)
	a.startTask()
	return a
}

func (a *ServerController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/server")

	g.Use(a.checkLogin)
	g.POST("/status", a.status)
	g.POST("/runtimeTrend", func(c *gin.Context) {
		since, err := strconv.ParseInt(c.DefaultQuery("since", "0"), 10, 64)
		if err != nil || since < 0 {
			c.JSON(400, gin.H{"success": false, "msg": "无效的趋势时间"})
			return
		}
		rows, err := service.RuntimeTrendSince(since)
		jsonObj(c, rows, err)
	})
	g.POST("/runtimeTrendSummary", func(c *gin.Context) {
		buckets, err := service.RuntimeTrendSummary(time.Now())
		jsonObj(c, buckets, err)
	})
	g.POST("/inboundHealth", func(c *gin.Context) {
		health, err := service.InboundHealthSummary(session.GetLoginUser(c).Id, time.Now())
		jsonObj(c, health, err)
	})
	g.POST("/getXrayVersion", a.getXrayVersion)
	g.POST("/installXray/:version", a.installXray)
}

func (a *ServerController) refreshStatus() {
	if !a.sampleMu.TryLock() {
		return
	}
	defer a.sampleMu.Unlock()
	a.statusMu.Lock()
	last, active := a.lastStatus, time.Since(a.lastGetStatusTime) <= 3*time.Minute
	a.statusMu.Unlock()
	if active {
		next := a.serverService.GetStatus(last)
		a.statusMu.Lock()
		a.lastStatus = next
		a.statusMu.Unlock()
	}
}

func (a *ServerController) startTask() {
	webServer := global.GetWebServer()
	c := webServer.GetCron()
	collector := &service.RuntimeCollector{}
	c.AddFunc("@every 10s", func() {
		if err := collector.Collect(); err != nil {
			logger.Warning("runtime trend:", err)
		}
	})
	refreshMonthly := func() {
		if _, err := a.serverService.RefreshMonthlyTraffic(); err != nil {
			logger.Warning("refresh monthly traffic task failed:", err)
		}
	}
	refreshMonthly()
	c.AddFunc("@every 10s", refreshMonthly)
	c.AddFunc("@every 2s", a.refreshStatus)
}

func (a *ServerController) status(c *gin.Context) {
	a.statusMu.Lock()
	a.lastGetStatusTime = time.Now()
	status := a.lastStatus
	a.statusMu.Unlock()
	if status != nil {
		// The ten-second collector continues after foreground polling pauses.
		// Attach its latest diagnosis instead of returning a stale cached flag.
		copy := *status
		copy.CPUHealth = service.CurrentCPUHealth()
		status = &copy
	}
	jsonObj(c, status, nil)
}

func (a *ServerController) getXrayVersion(c *gin.Context) {
	now := time.Now()
	if now.Sub(a.lastGetVersionsTime) <= time.Minute {
		jsonObj(c, a.lastVersions, nil)
		return
	}

	versions, err := a.serverService.GetXrayVersions()
	if err != nil {
		jsonMsg(c, "获取版本", err)
		return
	}

	a.lastVersions = versions
	a.lastGetVersionsTime = time.Now()

	jsonObj(c, versions, nil)
}

func (a *ServerController) installXray(c *gin.Context) {
	version := c.Param("version")
	err := a.serverService.UpdateXray(version)
	jsonMsg(c, "安装 xray", err)
}

func (a *ServerController) FlushMonthlyTraffic() error { return a.serverService.FlushMonthlyTraffic() }

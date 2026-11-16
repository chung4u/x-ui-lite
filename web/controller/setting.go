package controller

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"time"
	"x-ui/web/entity"
	"x-ui/web/service"
	"x-ui/web/session"
)

type updateUserForm struct {
	OldUsername string `json:"oldUsername" form:"oldUsername"`
	OldPassword string `json:"oldPassword" form:"oldPassword"`
	NewUsername string `json:"newUsername" form:"newUsername"`
	NewPassword string `json:"newPassword" form:"newPassword"`
}

type certificateInfoForm struct {
	CertFile string `json:"certFile" form:"certFile"`
}

type certificateGenerateForm struct {
	CommonName string `json:"commonName" form:"commonName"`
	ValidDays  int    `json:"validDays" form:"validDays"`
}

type SettingController struct {
	settingService service.SettingService
	userService    service.UserService
	panelService   service.PanelService
}

func NewSettingController(g *gin.RouterGroup) *SettingController {
	a := &SettingController{}
	a.initRouter(g)
	return a
}

func (a *SettingController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/setting")

	g.POST("/all", a.getAllSetting)
	g.POST("/applyState", a.getSettingApplyState)
	g.POST("/update", a.updateSetting)
	g.POST("/updateUser", a.updateUser)
	g.POST("/restartPanel", a.restartPanel)
	g.POST("/certificateInfo", a.getCertificateInfo)
	g.POST("/generateCertificate", a.generateCertificate)
}

func (a *SettingController) getSettingApplyState(c *gin.Context) {
	saved, err := a.settingService.GetAllSetting()
	if err != nil {
		jsonMsg(c, "读取配置生效状态", err)
		return
	}
	core := service.XrayService{}
	jsonObj(c, service.WithCoreApplyState(service.CurrentSettingApplyState(saved), core.GetApplyState()), nil)
}

func (a *SettingController) getAllSetting(c *gin.Context) {
	allSetting, err := a.settingService.GetAllSetting()
	if err != nil {
		jsonMsg(c, "获取设置", err)
		return
	}
	jsonObj(c, allSetting, nil)
}

func (a *SettingController) updateSetting(c *gin.Context) {
	allSetting := &entity.AllSetting{}
	err := c.ShouldBind(allSetting)
	if err != nil {
		jsonMsg(c, "修改设置", err)
		return
	}
	err = a.settingService.UpdateAllSetting(allSetting)
	jsonMsg(c, "修改设置", err)
}

func (a *SettingController) updateUser(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	form := &updateUserForm{}
	err := c.ShouldBind(form)
	if err != nil {
		jsonMsg(c, "修改用户", err)
		return
	}
	user := session.GetLoginUser(c)
	if user == nil || len(form.OldPassword) > 1024 || len(form.OldUsername) > 512 {
		jsonMsg(c, "修改用户", errors.New("原用户名或原密码错误"))
		return
	}
	if form.NewUsername == "" || form.NewPassword == "" {
		jsonMsg(c, "修改用户", errors.New("新用户名和新密码不能为空"))
		return
	}
	finish, allowed := session.BeginAuthentication(c, user.Username)
	if !allowed {
		return
	}
	defer finish()
	err = a.userService.ChangeCredentials(user.Id, form.OldUsername, form.OldPassword, form.NewUsername, form.NewPassword)
	if err == nil {
		err = session.ClearSession(c)
	}
	jsonMsg(c, "修改用户", err)
}

func (a *SettingController) restartPanel(c *gin.Context) {
	err := a.panelService.RestartPanel(time.Second * 3)
	jsonMsg(c, "重启面板", err)
}

func (a *SettingController) getCertificateInfo(c *gin.Context) {
	form := &certificateInfoForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, "读取证书信息", err)
		return
	}
	allSetting, err := a.settingService.GetAllSetting()
	if err != nil {
		jsonMsg(c, "读取证书信息", err)
		return
	}
	if form.CertFile != allSetting.WebCertFile && !strings.HasPrefix(form.CertFile, "/etc/x-ui/certs/panel-") {
		jsonMsg(c, "读取证书信息", errors.New("仅可读取当前或面板生成的证书"))
		return
	}
	info, err := a.settingService.GetCertificateInfo(form.CertFile)
	jsonObj(c, info, err)
}

func (a *SettingController) generateCertificate(c *gin.Context) {
	form := &certificateGenerateForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, "生成证书", err)
		return
	}
	result, err := a.settingService.GeneratePanelCertificate(form.CommonName, form.ValidDays)
	jsonObj(c, result, err)
}

package controller

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"x-ui/logger"
	"x-ui/web/service"
	"x-ui/web/session"
)

type LoginForm struct {
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`
}

type IndexController struct {
	BaseController

	userService service.UserService
}

func NewIndexController(g *gin.RouterGroup) *IndexController {
	a := &IndexController{}
	a.initRouter(g)
	return a
}

func (a *IndexController) initRouter(g *gin.RouterGroup) {
	g.GET("/", a.index)
	g.POST("/login", a.login)
	g.POST("/logout", a.logout)
}

func (a *IndexController) index(c *gin.Context) {
	if session.IsLogin(c) {
		c.Redirect(http.StatusTemporaryRedirect, "xui/")
		return
	}
	html(c, "login.html", "登录", nil)
}

func (a *IndexController) login(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	var form LoginForm
	err := c.ShouldBind(&form)
	if err != nil {
		pureJsonMsg(c, false, "数据格式错误")
		return
	}
	if form.Username == "" || len(form.Username) > 512 {
		pureJsonMsg(c, false, "请输入用户名")
		return
	}
	if form.Password == "" || len(form.Password) > 1024 {
		pureJsonMsg(c, false, "请输入密码")
		return
	}
	finish, allowed := session.BeginAuthentication(c, form.Username)
	if !allowed {
		return
	}
	defer finish()
	user := a.userService.CheckUser(form.Username, form.Password)
	if user == nil {
		logger.Info("administrator authentication failed")
		pureJsonMsg(c, false, "用户名或密码错误")
		return
	}

	err = session.SetLoginUser(c, user)
	if err == nil {
		logger.Info("user", user.Id, "login success")
	}
	jsonMsg(c, "登录", err)
}

func (a *IndexController) logout(c *gin.Context) {
	user := session.GetLoginUser(c)
	if user != nil {
		logger.Info("user", user.Id, "logout")
	}
	jsonMsg(c, "退出登录", session.ClearSession(c))
}

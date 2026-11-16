package main

import (
	"flag"
	"fmt"
	"github.com/op/go-logging"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "unsafe"
	"x-ui/config"
	"x-ui/database"
	"x-ui/logger"
	"x-ui/v2ui"
	"x-ui/web"
	"x-ui/web/global"
	"x-ui/web/service"
)

func runWebServer() {
	log.Printf("%v %v", config.GetName(), config.GetVersion())

	switch config.GetLogLevel() {
	case config.Debug:
		logger.InitLogger(logging.DEBUG)
	case config.Info:
		logger.InitLogger(logging.INFO)
	case config.Warn:
		logger.InitLogger(logging.WARNING)
	case config.Error:
		logger.InitLogger(logging.ERROR)
	default:
		log.Fatal("unknown log level:", config.GetLogLevel())
	}

	err := database.InitDB(config.GetDBPath())
	if err != nil {
		log.Fatal(err)
	}

	var server *web.Server

	server = web.NewServer()
	global.SetWebServer(server)
	err = server.Start()
	if err != nil {
		log.Println(err)
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)
	prepared := false
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGUSR1:
			path, receiptErr := maintenanceReceiptPath()
			if receiptErr != nil {
				logger.Warning("maintenance preparation refused:", receiptErr)
				continue
			}
			if err := server.Stop(); err != nil {
				logger.Warning("maintenance preparation failed; process retained:", err)
				continue
			}
			prepared = true
			if err := writeMaintenanceReceipt(path); err != nil {
				logger.Warning("maintenance receipt failed:", err)
				continue
			}
			logger.Info("maintenance prepared; traffic persisted and core stopped")
		case syscall.SIGHUP:
			if prepared {
				logger.Warning("reload refused while maintenance is prepared")
				continue
			}
			err := server.Stop()
			if err != nil {
				logger.Warning("stop server err:", err)
				continue
			}
			server = web.NewServer()
			global.SetWebServer(server)
			err = server.Start()
			if err != nil {
				log.Println(err)
				return
			}
		default:
			if !prepared {
				if err := server.Stop(); err != nil {
					logger.Warning("stop failed; process retained for traffic recovery:", err)
					continue
				}
			}
			return
		}
	}
}

func resetSetting() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	settingService := service.SettingService{}
	err = settingService.ResetSettings()
	if err != nil {
		fmt.Println("reset setting failed:", err)
	} else {
		fmt.Println("reset setting success")
	}
}

func updateSetting(port int, username string, password string, basePath string) error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return err
	}

	settingService := service.SettingService{}

	if port > 0 {
		err := settingService.SetPort(port)
		if err != nil {
			return fmt.Errorf("set port failed: %w", err)
		} else {
			fmt.Printf("set port %v success", port)
		}
	}
	if username != "" || password != "" {
		userService := service.UserService{}
		err := userService.UpdateFirstUser(username, password)
		if err != nil {
			return fmt.Errorf("set username and password failed: %w", err)
		} else {
			fmt.Println("set username and password success")
		}
	}
	if basePath != "" {
		err := settingService.SetBasePath(basePath)
		if err != nil {
			return fmt.Errorf("set base path failed: %w", err)
		} else {
			fmt.Printf("set base path %v success", basePath)
		}
	}
	return nil
}

func printPanelAccess() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println("读取面板访问配置失败:", err)
		return
	}

	settingService := service.SettingService{}
	port, err := settingService.GetPort()
	if err != nil {
		fmt.Println("读取面板端口失败:", err)
		return
	}
	basePath, err := settingService.GetBasePath()
	if err != nil {
		fmt.Println("读取面板访问路径失败:", err)
		return
	}
	certFile, certErr := settingService.GetCertFile()
	keyFile, keyErr := settingService.GetKeyFile()
	protocol := "http"
	if certErr == nil && keyErr == nil && certFile != "" && keyFile != "" {
		protocol = "https"
	}

	fmt.Printf("PANEL_PROTOCOL=%s\nPANEL_PORT=%d\nPANEL_BASE_PATH=%s\n", protocol, port, basePath)
}

func main() {
	if len(os.Args) < 2 {
		runWebServer()
		return
	}

	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "show version")

	runCmd := flag.NewFlagSet("run", flag.ExitOnError)

	v2uiCmd := flag.NewFlagSet("v2-ui", flag.ExitOnError)
	var dbPath string
	v2uiCmd.StringVar(&dbPath, "db", "/etc/v2-ui/v2-ui.db", "set v2-ui db file path")

	settingCmd := flag.NewFlagSet("setting", flag.ExitOnError)
	var port int
	var username string
	var password string
	var passwordStdin bool
	var basePath string
	var reset bool
	var showPanelAccess bool
	settingCmd.BoolVar(&reset, "reset", false, "reset all setting")
	settingCmd.IntVar(&port, "port", 0, "set panel port")
	settingCmd.StringVar(&username, "username", "", "set login username")
	settingCmd.StringVar(&password, "password", "", "set login password")
	settingCmd.BoolVar(&passwordStdin, "password-stdin", false, "read login password from stdin instead of process arguments")
	settingCmd.StringVar(&basePath, "base-path", "", "set panel base path")
	settingCmd.BoolVar(&showPanelAccess, "show-panel-access", false, "show panel access settings")

	oldUsage := flag.Usage
	flag.Usage = func() {
		oldUsage()
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("    run            run web panel")
		fmt.Println("    v2-ui          migrate form v2-ui")
		fmt.Println("    setting        set settings")
		fmt.Println("    check          validate effective Xray configuration without starting it")
	}

	flag.Parse()
	if showVersion {
		fmt.Println(config.GetVersion())
		return
	}

	switch os.Args[1] {
	case "check":
		if err := database.InitDB(config.GetDBPath()); err != nil {
			fmt.Println("Database check failed")
			os.Exit(1)
		}
		xrayService := service.XrayService{}
		if err := xrayService.CheckConfiguration(); err != nil {
			fmt.Println("Xray configuration validation failed")
			os.Exit(1)
		}
		fmt.Println("Xray configuration validation passed")
	case "run":
		err := runCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Println(err)
			return
		}
		runWebServer()
	case "v2-ui":
		err := v2uiCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Println(err)
			return
		}
		err = v2ui.MigrateFromV2UI(dbPath)
		if err != nil {
			fmt.Println("migrate from v2-ui failed:", err)
		}
	case "setting":
		err := settingCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Println(err)
			return
		}
		if passwordStdin {
			if password != "" {
				fmt.Println("cannot combine -password and -password-stdin")
				os.Exit(1)
			}
			data, err := io.ReadAll(io.LimitReader(os.Stdin, 1027))
			if err != nil {
				fmt.Println("cannot read password from stdin")
				os.Exit(1)
			}
			password = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
			if len(password) > 1024 {
				fmt.Println("password is too long")
				os.Exit(1)
			}
		}
		if reset {
			resetSetting()
		} else if showPanelAccess {
			printPanelAccess()
		} else {
			if err := updateSetting(port, username, password, basePath); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
	default:
		fmt.Println("except 'run' or 'v2-ui' or 'setting' subcommands")
		fmt.Println()
		runCmd.Usage()
		fmt.Println()
		v2uiCmd.Usage()
		fmt.Println()
		settingCmd.Usage()
	}
}

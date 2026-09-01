package job

import (
	"time"
	"x-ui/logger"
	"x-ui/web/service"
)

type XrayTrafficJob struct {
	xrayService    service.XrayService
	inboundService service.InboundService
}

func NewXrayTrafficJob() *XrayTrafficJob {
	return new(XrayTrafficJob)
}

func (j *XrayTrafficJob) Run() {
	needsRestart, err := j.inboundService.SyncMonthlyTrafficPeriod(time.Now())
	if err != nil {
		logger.Warning("sync inbound monthly traffic period failed:", err)
		return
	}
	if needsRestart {
		j.xrayService.SetToNeedRestart()
	}
	if !j.xrayService.IsXrayRunning() {
		return
	}
	traffics, err := j.xrayService.GetXrayTraffic()
	if err != nil {
		logger.Warning("get xray traffic failed:", err)
		return
	}
	err = j.inboundService.AddTraffic(traffics)
	if err != nil {
		logger.Warning("add traffic failed:", err)
	}
}

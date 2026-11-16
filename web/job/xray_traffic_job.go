package job

import (
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
	if err := j.xrayService.CollectTraffic(); err != nil {
		logger.Warning("collect and persist xray traffic failed:", err)
	}
}

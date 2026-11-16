package job

import (
	"x-ui/logger"
	"x-ui/web/service"
)

type CheckInboundJob struct {
	xrayService    service.XrayService
	inboundService service.InboundService
}

func NewCheckInboundJob() *CheckInboundJob {
	return new(CheckInboundJob)
}

func (j *CheckInboundJob) Run() {
	if err := j.xrayService.RefreshInboundPolicies(); err != nil {
		logger.Warning("refresh inbound and user policies failed:", err)
	}
}

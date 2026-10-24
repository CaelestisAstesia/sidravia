package contract

const (
	MethodUpdatePrepare     = "update.prepare"
	MethodUpdateStatus      = "update.status"
	MethodUpdateAcknowledge = "update.acknowledge"
	MethodUpdateCommit      = "update.commit"
	MethodUpdateResume      = "update.resume"
	MethodUpdateAbort       = "update.abort"
	ErrorCodeMaintenance    = "maintenance_active"
)

type UpdatePreparePayload struct {
	OperationID    string `json:"operationId"`
	CoordinatorPID uint32 `json:"coordinatorPid"`
	Interactive    *bool  `json:"interactive"`
}
type UpdateOperationPayload struct {
	OperationID string `json:"operationId"`
}
type UpdateAcknowledgePayload struct {
	OperationID string `json:"operationId"`
	Accepted    *bool  `json:"accepted"`
	Visible     *bool  `json:"visible"`
}

type UpdateResumePayload struct {
	OperationID     string `json:"operationId"`
	ConfigurationID string `json:"configurationId"`
}

package maintenance

type Intent struct {
	Mode            string `json:"mode"` // none, desktop, headless
	Visible         bool   `json:"visible"`
	Maintain        bool   `json:"maintain"`
	ConfigurationID string `json:"configurationId,omitempty"`
}

type View struct {
	SchemaVersion int    `json:"schemaVersion"`
	OperationID   string `json:"operationId,omitempty"`
	Phase         string `json:"phase"`
	Interactive   bool   `json:"interactive"`
	Intent        Intent `json:"intent"`
}

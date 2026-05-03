package models

type Graph struct {
	Nodes []GraphNode `json:"nodes" binding:"required"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID       string                 `json:"id" binding:"required"`
	Kind     string                 `json:"kind" binding:"required"`
	Label    string                 `json:"label"`
	Position Position               `json:"position"`
	Data     map[string]interface{} `json:"data"`
}

type GraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source" binding:"required"`
	Target string `json:"target" binding:"required"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type SingBoxConfig struct {
	Log       map[string]interface{}   `json:"log,omitempty"`
	DNS       map[string]interface{}   `json:"dns,omitempty"`
	Inbounds  []map[string]interface{} `json:"inbounds"`
	Outbounds []map[string]interface{} `json:"outbounds"`
	Route     map[string]interface{}   `json:"route"`
}

type SystemStatus struct {
	CPUPercent      float64 `json:"cpu_percent"`
	MemoryUsed      uint64  `json:"memory_used"`
	MemoryTotal     uint64  `json:"memory_total"`
	UptimeSeconds   uint64  `json:"uptime_seconds"`
	SingBoxRunning  bool    `json:"sing_box_running"`
	SingBoxPID      int     `json:"sing_box_pid,omitempty"`
	GeneratedAtUnix int64   `json:"generated_at_unix"`
}

package agent

type HostUsage struct {
	CPUPercent      *float64 `json:"cpu_percent,omitempty" doc:"Busy time as a percentage of all node CPUs"`
	MemoryTotal     *uint64  `json:"memory_total,omitempty" doc:"Total physical memory in bytes"`
	MemoryAvailable *uint64  `json:"memory_available,omitempty" doc:"Memory available to new processes in bytes"`
	DiskTotal       *uint64  `json:"disk_total,omitempty" doc:"Capacity of the Docker data filesystem in bytes"`
	DiskAvailable   *uint64  `json:"disk_available,omitempty" doc:"Available space on the Docker data filesystem in bytes"`
}

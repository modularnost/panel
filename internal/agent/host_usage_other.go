//go:build !linux

package agent

import "context"

func ReadHostUsage(context.Context) HostUsage { return HostUsage{} }

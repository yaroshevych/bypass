package run

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func callerAncestry(conn *net.UnixConn) []string {
	pid, err := peerPID(conn)
	if err != nil {
		return nil
	}
	var chain []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 6 && pid > 1; i++ {
		out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "ppid=,comm=").Output()
		if err != nil {
			break
		}
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			break
		}
		chain = append(chain, strconv.Itoa(pid)+" "+strings.Join(fields[1:], " "))
		next, err := strconv.Atoi(fields[0])
		if err != nil || next == pid {
			break
		}
		pid = next
	}
	return chain
}

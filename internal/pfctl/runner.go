// Package pfctl provides a safe, allowlisted interface to the OpenBSD
// pfctl(8) command, plus parsers for its text output.
package pfctl

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Command identifies an allowlisted read-only pfctl invocation.
type Command string

const (
	CmdInfo       Command = "info"
	CmdStates     Command = "states"
	CmdRules      Command = "rules"
	CmdInterfaces Command = "interfaces"
	CmdTables     Command = "tables"
)

// allowlist maps each command to the exact pfctl arguments it is allowed
// to run. Nothing outside this map can ever be executed. Keep this in
// sync with examples/doas.conf.
var allowlist = map[Command][]string{
	CmdInfo:       {"-s", "info"},
	CmdStates:     {"-s", "states"},
	CmdRules:      {"-s", "rules", "-v"},
	CmdInterfaces: {"-s", "Interfaces", "-v"},
	CmdTables:     {"-s", "Tables"},
}

// TableOp is an allowlisted PF table operation.
type TableOp string

const (
	TableShow   TableOp = "show"
	TableAdd    TableOp = "add"
	TableDelete TableOp = "delete"
)

// tableHelper is a small root-owned wrapper script (see
// examples/pfwebd-table) that re-validates its arguments and only ever
// runs "pfctl -t <table> -T show|add|delete [address]". Permitting this
// single script in doas.conf is much safer than permitting pfctl with
// arbitrary arguments.
const tableHelper = "/usr/local/sbin/pfwebd-table"

var tableNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,30}$`)

// ValidTableName reports whether s is a safe PF table name.
func ValidTableName(s string) bool {
	return tableNameRe.MatchString(s)
}

// ValidAddress reports whether s is a valid IP address or CIDR prefix.
func ValidAddress(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	if _, err := netip.ParsePrefix(s); err == nil {
		return true
	}
	return false
}

// Runner executes allowlisted pfctl operations and returns raw output.
type Runner interface {
	Run(ctx context.Context, cmd Command) (string, error)
	RunTable(ctx context.Context, op TableOp, table, address string) (string, error)
}

// LogStreamer produces a live stream of pflog lines.
type LogStreamer interface {
	StreamLogs(ctx context.Context) (<-chan string, error)
}

// tcpdumpArgv is the exact, fixed command used to read pflog0.
// Keep in sync with examples/doas.conf.
var tcpdumpArgv = []string{"/usr/sbin/tcpdump", "-n", "-e", "-ttt", "-l", "-i", "pflog0"}

// ExecRunner runs pfctl on the local system through doas(1).
// The daemon itself must run as an unprivileged user (e.g. _pfwebd)
// whose doas.conf only permits the exact commands in the allowlist
// plus the pfwebd-table helper script.
type ExecRunner struct {
	timeout time.Duration
}

func NewExecRunner(timeout time.Duration) *ExecRunner {
	return &ExecRunner{timeout: timeout}
}

func (r *ExecRunner) Run(ctx context.Context, cmd Command) (string, error) {
	args, ok := allowlist[cmd]
	if !ok {
		return "", fmt.Errorf("pfctl: command %q is not allowlisted", cmd)
	}
	return r.doas(ctx, append([]string{"/sbin/pfctl"}, args...))
}

func (r *ExecRunner) RunTable(ctx context.Context, op TableOp, table, address string) (string, error) {
	switch op {
	case TableShow, TableAdd, TableDelete:
	default:
		return "", fmt.Errorf("pfctl: table operation %q is not allowlisted", op)
	}
	if !ValidTableName(table) {
		return "", fmt.Errorf("pfctl: invalid table name %q", table)
	}
	argv := []string{tableHelper, string(op), table}
	if op != TableShow {
		if !ValidAddress(address) {
			return "", fmt.Errorf("pfctl: invalid address %q", address)
		}
		argv = append(argv, address)
	}
	return r.doas(ctx, argv)
}

// AnchorName is the PF anchor managed by the web UI. It is fixed
// because the doas.conf entries must match it exactly.
const AnchorName = "pfwebd"

// Fixed argument vectors for anchor operations.
// Keep in sync with examples/doas.conf.
var (
	anchorValidateArgv = []string{"/sbin/pfctl", "-a", AnchorName, "-n", "-f", "-"}
	anchorLoadArgv     = []string{"/sbin/pfctl", "-a", AnchorName, "-f", "-"}
	anchorFlushArgv    = []string{"/sbin/pfctl", "-a", AnchorName, "-F", "rules"}
	anchorShowArgv     = []string{"/sbin/pfctl", "-a", AnchorName, "-s", "rules"}
)

// AnchorValidate dry-runs a ruleset against the UI anchor (pfctl -n).
func (r *ExecRunner) AnchorValidate(ctx context.Context, rules string) error {
	_, err := r.doasStdin(ctx, anchorValidateArgv, rules)
	return err
}

// AnchorLoad replaces the UI anchor ruleset.
func (r *ExecRunner) AnchorLoad(ctx context.Context, rules string) error {
	_, err := r.doasStdin(ctx, anchorLoadArgv, rules)
	return err
}

// AnchorFlush removes every rule from the UI anchor.
func (r *ExecRunner) AnchorFlush(ctx context.Context) error {
	_, err := r.doas(ctx, anchorFlushArgv)
	return err
}

// AnchorRules returns the rules currently loaded in the UI anchor.
func (r *ExecRunner) AnchorRules(ctx context.Context) (string, error) {
	return r.doas(ctx, anchorShowArgv)
}

// StreamLogs starts tcpdump on pflog0 through doas and streams its
// stdout line by line. The process is killed when ctx is cancelled.
func (r *ExecRunner) StreamLogs(ctx context.Context) (<-chan string, error) {
	c := exec.CommandContext(ctx, "doas", tcpdumpArgv...)
	c.Stderr = io.Discard
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("tcpdump: %w", err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("tcpdump: %w", err)
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer c.Wait() // reap the process once the pipe is drained
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		for sc.Scan() {
			select {
			case ch <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (r *ExecRunner) doas(ctx context.Context, argv []string) (string, error) {
	return r.doasStdin(ctx, argv, "")
}

func (r *ExecRunner) doasStdin(ctx context.Context, argv []string, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "doas", argv...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("%v: %w: %s", argv, err, stderr.String())
	}
	return stdout.String(), nil
}

package pfctl

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// MockRunner replays realistic pfctl outputs so the web UI can be
// developed and demoed without an OpenBSD machine. Counters grow with
// time so the dashboard feels alive, and tables are kept in memory.
type MockRunner struct {
	start time.Time

	mu     sync.Mutex
	tables map[string]map[string]bool
	anchor []string
}

func NewMockRunner() *MockRunner {
	return &MockRunner{
		start: time.Now(),
		tables: map[string]map[string]bool{
			"blocklist": {
				"203.0.113.66":    true,
				"198.51.100.0/24": true,
				"192.0.2.41":      true,
			},
			"allowlist": {
				"192.168.1.0/24": true,
			},
		},
	}
}

func (m *MockRunner) Run(_ context.Context, cmd Command) (string, error) {
	switch cmd {
	case CmdInfo:
		return m.info(), nil
	case CmdStates:
		return mockStates, nil
	case CmdRules:
		return m.rules(), nil
	case CmdInterfaces:
		return mockInterfaces, nil
	case CmdTables:
		return m.tableNames(), nil
	}
	return "", fmt.Errorf("pfctl mock: unknown command %q", cmd)
}

func (m *MockRunner) RunTable(_ context.Context, op TableOp, table, address string) (string, error) {
	if !ValidTableName(table) {
		return "", fmt.Errorf("pfctl mock: invalid table name %q", table)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	addrs, ok := m.tables[table]
	if !ok {
		return "", fmt.Errorf("pfctl mock: table <%s> does not exist", table)
	}
	switch op {
	case TableShow:
		var list []string
		for a := range addrs {
			list = append(list, "   "+a)
		}
		sort.Strings(list)
		return strings.Join(list, "\n") + "\n", nil
	case TableAdd:
		if !ValidAddress(address) {
			return "", fmt.Errorf("pfctl mock: invalid address %q", address)
		}
		if addrs[address] {
			return "0/1 addresses added.\n", nil
		}
		addrs[address] = true
		return "1/1 addresses added.\n", nil
	case TableDelete:
		if !addrs[address] {
			return "0/1 addresses deleted.\n", nil
		}
		delete(addrs, address)
		return "1/1 addresses deleted.\n", nil
	}
	return "", fmt.Errorf("pfctl mock: unknown table operation %q", op)
}

var mockRuleStartRe = regexp.MustCompile(`^(pass|block|match)\b`)

// AnchorValidate mimics "pfctl -n -f -": basic per-line syntax check.
func (m *MockRunner) AnchorValidate(_ context.Context, rules string) error {
	for i, line := range strings.Split(rules, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !mockRuleStartRe.MatchString(line) {
			return fmt.Errorf("stdin:%d: syntax error", i+1)
		}
	}
	return nil
}

// AnchorLoad replaces the in-memory anchor ruleset.
func (m *MockRunner) AnchorLoad(ctx context.Context, rules string) error {
	if err := m.AnchorValidate(ctx, rules); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.anchor = nonEmptyLines(rules)
	return nil
}

// AnchorFlush clears the in-memory anchor.
func (m *MockRunner) AnchorFlush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.anchor = nil
	return nil
}

// AnchorRules returns the in-memory anchor content.
func (m *MockRunner) AnchorRules(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.anchor) == 0 {
		return "", nil
	}
	return strings.Join(m.anchor, "\n") + "\n", nil
}

// StreamLogs emits realistic synthetic pflog lines (tcpdump format)
// at random intervals until ctx is cancelled.
func (m *MockRunner) StreamLogs(ctx context.Context) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for {
			delay := time.Duration(400+r.Intn(1800)) * time.Millisecond
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			select {
			case ch <- mockLogLine(r):
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

var (
	mockScanners  = []string{"203.0.113.66", "198.51.100.23", "192.0.2.41", "203.0.113.250"}
	mockScanPorts = []int{22, 23, 80, 445, 3389, 8080, 5900}
	mockDests     = []string{"151.101.1.140", "140.82.121.4", "13.107.42.14", "9.9.9.9"}
)

func mockLogLine(r *rand.Rand) string {
	ts := time.Now().Format("Jan _2 15:04:05.000000")
	switch r.Intn(10) {
	case 0, 1, 2, 3, 4, 5: // blocked inbound scan
		src := mockScanners[r.Intn(len(mockScanners))]
		port := mockScanPorts[r.Intn(len(mockScanPorts))]
		seq := r.Intn(4000000000)
		return fmt.Sprintf("%s rule 1/(match) block in on em0: %s.%d > 81.65.12.34.%d: S %d:%d(0) win %d",
			ts, src, 1024+r.Intn(60000), port, seq, seq, 1024+r.Intn(64000))
	case 6, 7: // passed outbound tcp
		dst := mockDests[r.Intn(len(mockDests))]
		return fmt.Sprintf("%s rule 2/(match) pass out on em0: 192.168.1.100.%d > %s.443: S %d:%d(0) win 65535 <mss 1460,sackOK,eol>",
			ts, 49152+r.Intn(16000), dst, r.Intn(4000000000), r.Intn(4000000000))
	case 8: // passed dns
		return fmt.Sprintf("%s rule 2/(match) pass out on em0: 192.168.1.100.%d > 9.9.9.9.53: udp 34",
			ts, 49152+r.Intn(16000))
	default: // blocked icmp
		src := mockScanners[r.Intn(len(mockScanners))]
		return fmt.Sprintf("%s rule 1/(match) block in on em0: %s > 81.65.12.34: icmp: echo request",
			ts, src)
	}
}

func (m *MockRunner) tableNames() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for n := range m.tables {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "\n") + "\n"
}

func (m *MockRunner) info() string {
	elapsed := int64(time.Since(m.start).Seconds())
	searches := 1326778 + elapsed*17
	inserts := 17439 + elapsed/4
	removals := 17397 + elapsed/4
	matches := 19608 + elapsed/3
	current := 38 + elapsed%9

	return fmt.Sprintf(`Status: Enabled for 21 days 04:11:33              Debug: err

State Table                          Total             Rate
  current entries                     %4d
  half-open tcp                          2
  searches                        %8d           17.4/s
  inserts                         %8d            0.2/s
  removals                        %8d            0.2/s
Counters
  match                           %8d            0.3/s
  bad-offset                             0            0.0/s
  fragment                               0            0.0/s
  short                                  0            0.0/s
  normalize                              0            0.0/s
  memory                                 0            0.0/s
  bad-timestamp                          0            0.0/s
  congestion                             0            0.0/s
  ip-option                             10            0.0/s
  proto-cksum                            0            0.0/s
  state-mismatch                         4            0.0/s
  state-insert                           0            0.0/s
  state-limit                            0            0.0/s
  src-limit                              0            0.0/s
  synproxy                               0            0.0/s
  translate                              0            0.0/s
  no-route                               0            0.0/s
`, current, searches, inserts, removals, matches)
}

const mockStates = `all tcp 192.168.1.1:22 <- 192.168.1.100:53421       ESTABLISHED:ESTABLISHED
all udp 192.168.1.1:53 <- 192.168.1.100:40231       MULTIPLE:SINGLE
all tcp 192.168.1.1:8080 <- 192.168.1.100:51544       ESTABLISHED:ESTABLISHED
em0 tcp 192.168.1.100:50112 -> 203.0.113.5:443       ESTABLISHED:ESTABLISHED
em0 tcp 81.65.12.34:60123 (192.168.1.100:50113) -> 151.101.1.140:443       ESTABLISHED:ESTABLISHED
em0 tcp 81.65.12.34:60124 (192.168.1.100:50114) -> 140.82.121.4:443       FIN_WAIT_2:FIN_WAIT_2
em0 udp 81.65.12.34:7891 (192.168.1.100:5353) -> 9.9.9.9:53       MULTIPLE:SINGLE
em0 udp 81.65.12.34:7892 (192.168.1.100:5354) -> 9.9.9.9:53       MULTIPLE:SINGLE
em0 icmp 81.65.12.34:8 -> 1.1.1.1:8       0:0
em0 tcp 81.65.12.34:60200 (192.168.1.100:50200) -> 13.107.42.14:443       ESTABLISHED:ESTABLISHED
`

func (m *MockRunner) rules() string {
	elapsed := int64(time.Since(m.start).Seconds())
	return fmt.Sprintf(`block return log all
  [ Evaluations: %d     Packets: 312       Bytes: 24960       States: 0     ]
pass out quick on em0 inet keep state
  [ Evaluations: %d     Packets: 981223    Bytes: 1073741824  States: 38    ]
pass in on em0 inet proto tcp from 192.168.1.0/24 to (em0) port = 22 flags S/SA keep state
  [ Evaluations: 1322      Packets: 4521      Bytes: 812332      States: 2     ]
pass in on em0 inet proto tcp from 192.168.1.0/24 to (em0) port = 8080 flags S/SA keep state
  [ Evaluations: 254       Packets: 1981      Bytes: 402113      States: 1     ]
anchor "pfwebd/*" all
  [ Evaluations: 18526     Packets: 0         Bytes: 0           States: 0     ]
`, 18526+elapsed/3, 17204+elapsed/3)
}

const mockInterfaces = `all
	Cleared:     Thu May 21 09:12:44 2026
	References:  [ States:  42                Rules: 5                 ]
	In4/Pass:    [ Packets: 1623441           Bytes: 1426771233        ]
	In4/Block:   [ Packets: 312               Bytes: 24960             ]
	Out4/Pass:   [ Packets: 981223            Bytes: 1073741824        ]
	Out4/Block:  [ Packets: 0                 Bytes: 0                 ]
em0
	Cleared:     Thu May 21 09:12:44 2026
	References:  [ States:  39                Rules: 4                 ]
	In4/Pass:    [ Packets: 1520112           Bytes: 1322004112        ]
	In4/Block:   [ Packets: 298               Bytes: 23840             ]
	Out4/Pass:   [ Packets: 922110            Bytes: 1002233445        ]
	Out4/Block:  [ Packets: 0                 Bytes: 0                 ]
lo0
	Cleared:     Thu May 21 09:12:44 2026
	References:  [ States:  3                 Rules: 0                 ]
	In4/Pass:    [ Packets: 103329            Bytes: 104767121         ]
	In4/Block:   [ Packets: 0                 Bytes: 0                 ]
	Out4/Pass:   [ Packets: 103329            Bytes: 104767121         ]
	Out4/Block:  [ Packets: 0                 Bytes: 0                 ]
`

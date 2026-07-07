package pfctl

import "testing"

const sampleInfo = `Status: Enabled for 0 days 21:11:33           Debug: err

State Table                          Total             Rate
  current entries                       42
  searches                         1326778           17.4/s
  inserts                            17439            0.2/s
  removals                           17397            0.2/s
Counters
  match                              19608            0.3/s
  bad-offset                             0            0.0/s
  state-mismatch                         4            0.0/s
`

func TestParseInfo(t *testing.T) {
	info := ParseInfo(sampleInfo)
	if !info.Enabled {
		t.Error("expected Enabled to be true")
	}
	if info.Since != "0 days 21:11:33" {
		t.Errorf("unexpected Since: %q", info.Since)
	}
	if len(info.StateTable) != 4 {
		t.Fatalf("expected 4 state table stats, got %d", len(info.StateTable))
	}
	if info.StateTable[0].Name != "current entries" || info.StateTable[0].Total != 42 {
		t.Errorf("unexpected first stat: %+v", info.StateTable[0])
	}
	if info.StateTable[1].Rate != 17.4 {
		t.Errorf("expected searches rate 17.4, got %v", info.StateTable[1].Rate)
	}
	if len(info.Counters) != 3 {
		t.Fatalf("expected 3 counters, got %d", len(info.Counters))
	}
	if info.Counters[2].Name != "state-mismatch" || info.Counters[2].Total != 4 {
		t.Errorf("unexpected counter: %+v", info.Counters[2])
	}
}

func TestParseInfoDisabled(t *testing.T) {
	info := ParseInfo("Status: Disabled for 0 days 00:01:02           Debug: err\n")
	if info.Enabled {
		t.Error("expected Enabled to be false")
	}
}

const sampleStates = `all tcp 192.168.1.1:22 <- 192.168.1.100:53421       ESTABLISHED:ESTABLISHED
em0 tcp 81.65.12.34:60123 (192.168.1.100:50113) -> 151.101.1.140:443       ESTABLISHED:ESTABLISHED
em0 icmp 81.65.12.34:8 -> 1.1.1.1:8       0:0
`

func TestParseStates(t *testing.T) {
	states := ParseStates(sampleStates)
	if len(states) != 3 {
		t.Fatalf("expected 3 states, got %d", len(states))
	}
	s := states[0]
	if s.Interface != "all" || s.Proto != "tcp" || s.Direction != "<-" {
		t.Errorf("unexpected state[0]: %+v", s)
	}
	if s.Source != "192.168.1.1:22" || s.Destination != "192.168.1.100:53421" {
		t.Errorf("unexpected endpoints: %+v", s)
	}
	nat := states[1]
	if nat.Source != "81.65.12.34:60123 (192.168.1.100:50113)" {
		t.Errorf("NAT source not kept together: %q", nat.Source)
	}
	if nat.State != "ESTABLISHED:ESTABLISHED" {
		t.Errorf("unexpected state: %q", nat.State)
	}
}

const sampleRules = `block return log all
  [ Evaluations: 18526     Packets: 312       Bytes: 24960       States: 0     ]
pass out quick on em0 inet keep state
  [ Evaluations: 17204     Packets: 981223    Bytes: 1073741824  States: 38    ]
anchor "pfwebd/*" all
  [ Evaluations: 18526     Packets: 0         Bytes: 0           States: 0     ]
`

func TestParseRules(t *testing.T) {
	rules := ParseRules(sampleRules)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].Text != "block return log all" {
		t.Errorf("unexpected rule text: %q", rules[0].Text)
	}
	if rules[0].Evaluations != 18526 || rules[0].Packets != 312 {
		t.Errorf("unexpected rule stats: %+v", rules[0])
	}
	if rules[1].Bytes != 1073741824 || rules[1].States != 38 {
		t.Errorf("unexpected rule stats: %+v", rules[1])
	}
}

func TestMockOutputsParse(t *testing.T) {
	m := NewMockRunner()
	out, err := m.Run(nil, CmdInfo)
	if err != nil {
		t.Fatal(err)
	}
	info := ParseInfo(out)
	if !info.Enabled || len(info.StateTable) == 0 || len(info.Counters) == 0 {
		t.Errorf("mock info did not parse: %+v", info)
	}

	out, _ = m.Run(nil, CmdStates)
	if states := ParseStates(out); len(states) != 10 {
		t.Errorf("expected 10 mock states, got %d", len(states))
	}

	out, _ = m.Run(nil, CmdRules)
	if rules := ParseRules(out); len(rules) != 5 {
		t.Errorf("expected 5 mock rules, got %d", len(rules))
	}
}

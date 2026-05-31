package core

// RunOpts carries per-call flags through install/wire/unwire.
type RunOpts struct {
	DryRun  bool
	Upgrade bool
}

// Channel is where a tool is distributed from.
type Channel string

const (
	ChannelNpm    Channel = "npm"
	ChannelGitHub Channel = "github"
	ChannelCargo  Channel = "cargo"
	ChannelBinary Channel = "binary"
)

// Detection is the result of probing whether an agent is present.
type Detection struct {
	Installed bool
	Source    string // "cli" | "config" | ""
}

// AgentManifest describes one supported agent.
type AgentManifest struct {
	ID        string
	Label     string
	Homepage  string
	CLIBin    string
	ConfigDir func() string
	Detect    func() Detection
}

// AgentFn wires/unwires one tool for one agent.
type AgentFn func(opts RunOpts) (bool, error)

// VerifyFn confirms a tool is usable by an agent; nil result means "unknown".
type VerifyFn func() *bool

// ToolManifest describes one tool and how it installs/wires per agent.
type ToolManifest struct {
	ID          string
	Label       string
	Description string
	Homepage    string
	InstallHint string
	Channel     Channel
	Install     func(opts RunOpts) (bool, error)
	WireFor     map[string]AgentFn
	UnwireFor   map[string]AgentFn
	VerifyFor   map[string]VerifyFn
}

// registries are global and populated at startup by agents/tools packages.
var (
	agentList []*AgentManifest
	toolList  []*ToolManifest
	agentByID = map[string]*AgentManifest{}
	toolByID  = map[string]*ToolManifest{}
)

// RegisterAgent appends an agent, preserving registration order.
func RegisterAgent(a *AgentManifest) {
	if _, ok := agentByID[a.ID]; !ok {
		agentList = append(agentList, a)
	}
	agentByID[a.ID] = a
}

// RegisterTool appends a tool, preserving registration order.
func RegisterTool(t *ToolManifest) {
	if _, ok := toolByID[t.ID]; !ok {
		toolList = append(toolList, t)
	}
	toolByID[t.ID] = t
}

func ListAgents() []*AgentManifest { return agentList }
func ListTools() []*ToolManifest   { return toolList }

func GetAgent(id string) *AgentManifest { return agentByID[id] }
func GetTool(id string) *ToolManifest   { return toolByID[id] }

func AgentIDs() []string {
	ids := make([]string, len(agentList))
	for i, a := range agentList {
		ids[i] = a.ID
	}
	return ids
}

func ToolIDs() []string {
	ids := make([]string, len(toolList))
	for i, t := range toolList {
		ids[i] = t.ID
	}
	return ids
}

// BoolPtr is a helper for VerifyFn results.
func BoolPtr(b bool) *bool { return &b }

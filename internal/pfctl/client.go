package pfctl

import (
	"context"
	"strings"
)

// Client provides typed, parsed access to pfctl through a Runner.
type Client struct {
	r Runner
}

func NewClient(r Runner) *Client {
	return &Client{r: r}
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	out, err := c.r.Run(ctx, CmdInfo)
	if err != nil {
		return Info{}, err
	}
	return ParseInfo(out), nil
}

func (c *Client) States(ctx context.Context) ([]State, error) {
	out, err := c.r.Run(ctx, CmdStates)
	if err != nil {
		return nil, err
	}
	return ParseStates(out), nil
}

func (c *Client) Rules(ctx context.Context) ([]Rule, error) {
	out, err := c.r.Run(ctx, CmdRules)
	if err != nil {
		return nil, err
	}
	return ParseRules(out), nil
}

// InterfacesRaw returns the raw output of "pfctl -s Interfaces -v".
// The format varies between OpenBSD versions, so v1 displays it as-is.
func (c *Client) InterfacesRaw(ctx context.Context) (string, error) {
	return c.r.Run(ctx, CmdInterfaces)
}

// Tables lists PF table names ("pfctl -s Tables").
func (c *Client) Tables(ctx context.Context) ([]string, error) {
	out, err := c.r.Run(ctx, CmdTables)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

// TableAddresses lists the addresses of one table ("pfctl -t X -T show").
func (c *Client) TableAddresses(ctx context.Context, table string) ([]string, error) {
	out, err := c.r.RunTable(ctx, TableShow, table, "")
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

// TableAdd adds an address or CIDR prefix to a table.
func (c *Client) TableAdd(ctx context.Context, table, address string) error {
	_, err := c.r.RunTable(ctx, TableAdd, table, address)
	return err
}

// TableDelete removes an address or CIDR prefix from a table.
func (c *Client) TableDelete(ctx context.Context, table, address string) error {
	_, err := c.r.RunTable(ctx, TableDelete, table, address)
	return err
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

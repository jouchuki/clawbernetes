/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"html"
	"strings"
)

type agentInfo struct {
	Name           string
	Phase          string
	Model          string
	FallbackModel  string
	Provider       string
	Gateway        string
	Policy         string
	SkillSet       string
	Observability  string
	Connectors     []string
	Soul           string // first 100 chars
	BudgetDaily    int
	BudgetMonthly  int
	DowngradeModel string
	ToolDeny       []string
	RestartPolicy  string
	MaxRuntime     string
	IdleHibernate  int
}

func generateFleetDashboardHTML(agents []agentInfo) string {
	// Count agents by phase
	totalAgents := len(agents)
	phaseCount := map[string]int{}
	for _, a := range agents {
		phaseCount[a.Phase]++
	}

	// Build agent cards
	var agentCards strings.Builder
	for _, a := range agents {
		nameColor := "#e0e0e0"
		switch a.Phase {
		case "Running":
			nameColor = "#4ecca3"
		case "Pending":
			nameColor = "#ffd369"
		case "Error":
			nameColor = "#fc5185"
		}

		// Connectors
		connectorsHTML := "<span class=\"dim\">none</span>"
		if len(a.Connectors) > 0 {
			escaped := make([]string, len(a.Connectors))
			for i, c := range a.Connectors {
				escaped[i] = "<span class=\"tag\">" + html.EscapeString(c) + "</span>"
			}
			connectorsHTML = strings.Join(escaped, " ")
		}

		// Tool deny
		toolDenyHTML := "<span class=\"dim\">none</span>"
		if len(a.ToolDeny) > 0 {
			escaped := make([]string, len(a.ToolDeny))
			for i, t := range a.ToolDeny {
				escaped[i] = "<span class=\"tag deny\">" + html.EscapeString(t) + "</span>"
			}
			toolDenyHTML = strings.Join(escaped, " ")
		}

		// Fallback model display
		fallbackHTML := "<span class=\"dim\">none</span>"
		if a.FallbackModel != "" {
			fallbackHTML = html.EscapeString(a.FallbackModel)
		}

		// Soul snippet
		soulHTML := ""
		if a.Soul != "" {
			soulHTML = fmt.Sprintf("<div class=\"soul\"><em>\"%s\"</em></div>", html.EscapeString(a.Soul))
		}

		// Downgrade model
		downgradeHTML := "<span class=\"dim\">none</span>"
		if a.DowngradeModel != "" {
			downgradeHTML = html.EscapeString(a.DowngradeModel)
		}

		agentCards.WriteString(fmt.Sprintf(`<div class="agent-card">
  <div class="agent-header">
    <span class="agent-name" style="color:%s">%s</span>
    <span class="phase-badge phase-%s">%s</span>
  </div>
  <div class="agent-body">
    <div class="field-group">
      <h4>Model</h4>
      <div class="field"><span class="label">Provider:</span> %s</div>
      <div class="field"><span class="label">Model:</span> %s</div>
      <div class="field"><span class="label">Fallback:</span> %s</div>
    </div>
    <div class="field-group">
      <h4>References</h4>
      <div class="field"><span class="label">Gateway:</span> %s</div>
      <div class="field"><span class="label">Policy:</span> %s</div>
      <div class="field"><span class="label">SkillSet:</span> %s</div>
      <div class="field"><span class="label">Observability:</span> %s</div>
    </div>
    <div class="field-group">
      <h4>Connectors</h4>
      <div class="field">%s</div>
    </div>
    <div class="field-group">
      <h4>Budget</h4>
      <div class="field"><span class="label">Daily:</span> $%d</div>
      <div class="field"><span class="label">Monthly:</span> $%d</div>
      <div class="field"><span class="label">Downgrade to:</span> %s</div>
    </div>
    <div class="field-group">
      <h4>Tool Deny List</h4>
      <div class="field">%s</div>
    </div>
    <div class="field-group">
      <h4>Lifecycle</h4>
      <div class="field"><span class="label">Restart:</span> %s</div>
      <div class="field"><span class="label">Max Runtime:</span> %s</div>
      <div class="field"><span class="label">Idle Hibernate:</span> %d min</div>
    </div>
    %s
  </div>
</div>
`,
			nameColor,
			html.EscapeString(a.Name),
			html.EscapeString(strings.ToLower(a.Phase)),
			html.EscapeString(a.Phase),
			html.EscapeString(a.Provider),
			html.EscapeString(a.Model),
			fallbackHTML,
			html.EscapeString(a.Gateway),
			html.EscapeString(a.Policy),
			html.EscapeString(a.SkillSet),
			html.EscapeString(a.Observability),
			connectorsHTML,
			a.BudgetDaily,
			a.BudgetMonthly,
			downgradeHTML,
			toolDenyHTML,
			html.EscapeString(a.RestartPolicy),
			html.EscapeString(a.MaxRuntime),
			a.IdleHibernate,
			soulHTML,
		))
	}

	// Phase summary cards
	var phaseCards strings.Builder
	for _, phase := range []string{"Running", "Pending", "Error"} {
		count := phaseCount[phase]
		if count == 0 && phase == "Error" {
			continue
		}
		color := "#e0e0e0"
		switch phase {
		case "Running":
			color = "#4ecca3"
		case "Pending":
			color = "#ffd369"
		case "Error":
			color = "#fc5185"
		}
		phaseCards.WriteString(fmt.Sprintf(
			`<div class="summary-card"><div class="summary-value" style="color:%s">%d</div><div class="summary-label">%s</div></div>`+"\n",
			color, count, phase,
		))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Clawbernetes Fleet Configuration</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #1a1a2e;
    color: #e0e0e0;
    padding: 2rem;
    line-height: 1.6;
  }
  h1 {
    text-align: center;
    font-size: 1.8rem;
    margin-bottom: 0.3rem;
    color: #4ecca3;
  }
  .subtitle {
    text-align: center;
    color: #888;
    margin-bottom: 2rem;
    font-size: 0.9rem;
  }
  h2 {
    font-size: 1.3rem;
    margin: 2rem 0 1rem;
    color: #4ecca3;
    border-bottom: 1px solid #0f3460;
    padding-bottom: 0.4rem;
  }
  h4 {
    color: #4ecca3;
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-bottom: 0.4rem;
    border-bottom: 1px solid #0f3460;
    padding-bottom: 0.2rem;
  }

  /* Summary cards */
  .summary-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 1rem;
    margin-bottom: 1rem;
  }
  .summary-card {
    background: #16213e;
    border: 1px solid #0f3460;
    border-radius: 8px;
    padding: 1.2rem;
    text-align: center;
  }
  .summary-value {
    font-size: 2.4rem;
    font-weight: 700;
  }
  .summary-label {
    font-size: 0.85rem;
    color: #888;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* Agent cards */
  .agents-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
    gap: 1.2rem;
  }
  .agent-card {
    background: #16213e;
    border: 1px solid #0f3460;
    border-radius: 8px;
    overflow: hidden;
  }
  .agent-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.8rem 1rem;
    background: #0f3460;
  }
  .agent-name {
    font-size: 1.2rem;
    font-weight: 700;
  }
  .phase-badge {
    font-size: 0.75rem;
    font-weight: 600;
    padding: 0.2rem 0.6rem;
    border-radius: 12px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .phase-running  { background: rgba(78,204,163,0.2); color: #4ecca3; }
  .phase-pending  { background: rgba(255,211,105,0.2); color: #ffd369; }
  .phase-error    { background: rgba(252,81,133,0.2);  color: #fc5185; }
  .agent-body {
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
  }
  .field-group { }
  .field {
    font-size: 0.88rem;
    padding: 0.15rem 0;
  }
  .label {
    color: #888;
    display: inline-block;
    min-width: 110px;
  }
  .dim { color: #555; font-style: italic; }
  .tag {
    display: inline-block;
    background: rgba(78,204,163,0.15);
    color: #4ecca3;
    padding: 0.1rem 0.5rem;
    border-radius: 4px;
    font-size: 0.82rem;
    margin: 0.1rem 0.2rem 0.1rem 0;
  }
  .tag.deny {
    background: rgba(252,81,133,0.15);
    color: #fc5185;
  }
  .soul {
    margin-top: 0.4rem;
    padding: 0.6rem 0.8rem;
    background: rgba(15,52,96,0.5);
    border-left: 3px solid #0f3460;
    border-radius: 0 4px 4px 0;
    font-size: 0.85rem;
    color: #aaa;
  }

  /* How it works */
  .how-it-works {
    background: #16213e;
    border: 1px solid #0f3460;
    border-radius: 8px;
    padding: 1.2rem 1.5rem;
    margin-top: 1rem;
  }
  .how-it-works p {
    margin: 0.6rem 0;
    font-size: 0.92rem;
  }
  .how-it-works .arrow-flow {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    margin: 1rem 0;
    font-size: 0.88rem;
  }
  .how-it-works .step {
    background: #0f3460;
    padding: 0.4rem 0.8rem;
    border-radius: 6px;
    white-space: nowrap;
  }
  .how-it-works .arrow {
    color: #4ecca3;
    font-weight: 700;
  }
  pre.yaml {
    background: #111;
    border: 1px solid #333;
    border-radius: 6px;
    padding: 1rem;
    overflow-x: auto;
    font-family: "JetBrains Mono", "Fira Code", "Cascadia Code", Consolas, monospace;
    font-size: 0.82rem;
    line-height: 1.5;
    color: #c8d6e5;
    margin: 0.8rem 0;
  }
</style>
</head>
<body>

<h1>Clawbernetes Fleet Configuration</h1>
<p class="subtitle">Real-time overview of all managed AI agents in the cluster</p>

<h2>Fleet Summary</h2>
<div class="summary-grid">
  <div class="summary-card">
    <div class="summary-value" style="color:#4ecca3">%d</div>
    <div class="summary-label">Total Agents</div>
  </div>
%s</div>

<h2>Agent Fleet</h2>
<div class="agents-grid">
%s</div>

<h2>How It Works</h2>
<div class="how-it-works">
  <p>Each AI agent in the fleet is declared as a <strong>ClawAgent</strong> custom resource.
     The Clawbernetes operator watches for these CRs and reconciles the desired state.</p>

  <div class="arrow-flow">
    <span class="step">Apply ClawAgent CR</span>
    <span class="arrow">&#x2192;</span>
    <span class="step">Operator creates Pod with identity, skills, observeclaw plugin config</span>
    <span class="arrow">&#x2192;</span>
    <span class="step">Agent routes LLM calls through ClawGateway</span>
    <span class="arrow">&#x2192;</span>
    <span class="step">Traces flow to Tempo</span>
    <span class="arrow">&#x2192;</span>
    <span class="step">Visible in Grafana</span>
  </div>

  <p>Sample <strong>ClawAgent</strong> YAML:</p>
<pre class="yaml">apiVersion: claw.clawbernetes.io/v1
kind: ClawAgent
metadata:
  name: eng-agent
  namespace: clawbernetes
spec:
  identity:
    soul: |
      You are a senior software engineer with deep expertise
      in distributed systems and Kubernetes.
  skillSet: engineering-skills
  policy: engineering-policy
  gateway: main-gateway
  observability: fleet-observability
  connectors:
    - production-postgres
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    fallback:
      provider: anthropic
      name: claude-haiku-4-5
  lifecycle:
    restartPolicy: Always
    hibernateAfterIdleMinutes: 30
    maxRuntime: "24h"</pre>

  <p>The operator translates this into a running Pod with the correct environment,
     secrets, and sidecar configuration. Budget policies, tool restrictions, and
     lifecycle rules are enforced automatically.</p>
</div>

</body>
</html>`,
		totalAgents,
		phaseCards.String(),
		agentCards.String(),
	)
}

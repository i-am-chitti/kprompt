import * as vscode from "vscode";
import { isMutatingPlan, type PlanResultDoc } from "./planResult";

function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function riskClass(level: string | undefined, denied: boolean | undefined): string {
  if (denied) return "risk-denied";
  switch ((level ?? "").toLowerCase()) {
    case "high":
      return "risk-high";
    case "low":
      return "risk-low";
    default:
      return "risk-med";
  }
}

export function renderPlanResultHtml(
  doc: PlanResultDoc,
  nonce: string,
  cspSource: string
): string {
  const risk = doc.risk ?? {};
  const plan = doc.plan ?? {};
  const actions = plan.actions ?? [];
  const mutating = isMutatingPlan(doc);
  const canApprove = mutating && !risk.denied && !doc.applied;

  const actionBlocks = actions
    .map((a, i) => {
      const title = [
        a.op,
        a.kind && a.name ? `${a.kind}/${a.name}` : a.kind || a.name,
        a.namespace ? `ns/${a.namespace}` : "",
      ]
        .filter(Boolean)
        .join(" · ");
      const diff = a.diff?.trim()
        ? `<pre class="diff">${esc(a.diff)}</pre>`
        : `<p class="muted">No diff on this action.</p>`;
      return `<section class="card"><h3>${esc(title || `Action ${i + 1}`)}</h3>${diff}</section>`;
    })
    .join("\n");

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>PlanResult</title>
  <style>
    :root {
      --fg: var(--vscode-foreground);
      --muted: var(--vscode-descriptionForeground);
      --border: var(--vscode-panel-border, #444);
      --surface: var(--vscode-editor-background);
      --brand: var(--vscode-button-background);
      --brand-fg: var(--vscode-button-foreground);
      --danger: var(--vscode-errorForeground, #f87171);
      --ok: var(--vscode-testing-iconPassed, #34d399);
      --warn: var(--vscode-editorWarning-foreground, #fbbf24);
      --font: var(--vscode-font-family);
      --mono: var(--vscode-editor-font-family, ui-monospace, monospace);
    }
    body {
      margin: 0;
      padding: 1.25rem 1.5rem 2rem;
      color: var(--fg);
      font-family: var(--font);
      background: var(--surface);
      line-height: 1.45;
    }
    .eyebrow {
      font-family: var(--mono);
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--muted);
      margin: 0 0 0.5rem;
    }
    h1 {
      font-size: 1.35rem;
      font-weight: 600;
      margin: 0 0 0.75rem;
    }
    h2, h3 {
      font-size: 0.75rem;
      font-family: var(--mono);
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
      margin: 1.25rem 0 0.5rem;
    }
    h3 { margin-top: 0; color: var(--fg); text-transform: none; letter-spacing: 0; font-size: 0.9rem; }
    .muted { color: var(--muted); font-size: 0.9rem; }
    .meta {
      display: grid;
      gap: 0.35rem 1rem;
      grid-template-columns: auto 1fr;
      font-size: 0.9rem;
      margin: 0.75rem 0 1rem;
    }
    .meta dt { color: var(--muted); }
    .meta dd { margin: 0; font-family: var(--mono); font-size: 0.85rem; }
    .badge {
      display: inline-block;
      font-family: var(--mono);
      font-size: 11px;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      padding: 0.15rem 0.45rem;
      border: 1px solid var(--border);
      border-radius: 4px;
    }
    .risk-denied, .risk-high { color: var(--danger); border-color: var(--danger); }
    .risk-med { color: var(--warn); }
    .risk-low { color: var(--ok); }
    .card {
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 0.75rem 1rem;
      margin: 0.5rem 0;
    }
    pre.diff {
      margin: 0.5rem 0 0;
      padding: 0.75rem;
      overflow: auto;
      max-height: 18rem;
      font-family: var(--mono);
      font-size: 12px;
      line-height: 1.4;
      white-space: pre-wrap;
      border: 1px solid var(--border);
      border-radius: 6px;
      background: color-mix(in srgb, var(--surface) 80%, #000);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem;
      margin-top: 1.25rem;
    }
    button {
      font: inherit;
      cursor: pointer;
      border-radius: 6px;
      border: 1px solid transparent;
      padding: 0.45rem 0.9rem;
      background: var(--brand);
      color: var(--brand-fg);
    }
    button.secondary {
      background: transparent;
      color: var(--fg);
      border-color: var(--border);
    }
    button:disabled {
      opacity: 0.45;
      cursor: not-allowed;
    }
    .note {
      margin-top: 1rem;
      font-size: 0.85rem;
      color: var(--muted);
    }
  </style>
</head>
<body>
  <p class="eyebrow">PlanResult · not a chat</p>
  <h1>${esc(plan.summary || doc.prompt || "PlanResult")}</h1>
  <p>
    <span class="badge ${riskClass(risk.level, risk.denied)}">
      ${esc(risk.denied ? "denied" : risk.level || "risk ?")}
    </span>
    ${doc.applied ? '<span class="badge risk-low">applied</span>' : ""}
    ${plan.requiresApproval ? '<span class="badge">needs approve</span>' : ""}
  </p>
  <dl class="meta">
    ${doc.prompt ? `<dt>Prompt</dt><dd>${esc(doc.prompt)}</dd>` : ""}
    ${plan.intent ? `<dt>Intent</dt><dd>${esc(plan.intent)}</dd>` : ""}
    ${plan.namespace || doc.cluster_context ? `<dt>Scope</dt><dd>${esc([plan.namespace && `ns/${plan.namespace}`, doc.cluster_context || plan.context].filter(Boolean).join(" · "))}</dd>` : ""}
    ${risk.message ? `<dt>Risk note</dt><dd>${esc(risk.message)}</dd>` : ""}
  </dl>

  <h2>Actions</h2>
  ${actions.length ? actionBlocks : `<p class="muted">No actions in this PlanResult.</p>`}

  <div class="actions">
    <button id="approve" ${canApprove ? "" : "disabled"} title="Runs kprompt with --approve in a terminal after confirm">
      Approve via CLI
    </button>
    <button id="dismiss" class="secondary">Close</button>
  </div>
  <p class="note">
    Approve only re-runs the prompt through the local CLI with <code>--approve</code>.
    The extension never talks to the apiserver and never auto-applies.
  </p>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    document.getElementById('approve')?.addEventListener('click', () => {
      vscode.postMessage({ type: 'approve' });
    });
    document.getElementById('dismiss')?.addEventListener('click', () => {
      vscode.postMessage({ type: 'dismiss' });
    });
  </script>
</body>
</html>`;
}

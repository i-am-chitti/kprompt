import * as vscode from "vscode";
import { parsePlanResult, type PlanResultDoc } from "./planResult";
import { renderPlanResultHtml } from "./webviewHtml";

let lastDoc: PlanResultDoc | null = null;
let panel: vscode.WebviewPanel | undefined;

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(
    vscode.commands.registerCommand("kprompt.openPlanResult", async (uri?: vscode.Uri) => {
      const docUri = uri ?? vscode.window.activeTextEditor?.document.uri;
      if (!docUri) {
        void vscode.window.showWarningMessage(
          "Open a PlanResult JSON file first (kprompt -o json)."
        );
        return;
      }
      const textDoc = await vscode.workspace.openTextDocument(docUri);
      const parsed = parsePlanResult(textDoc.getText());
      if (!parsed) {
        void vscode.window.showErrorMessage(
          "Not a kprompt PlanResult (expected kind PlanResult with plan/risk)."
        );
        return;
      }
      lastDoc = parsed;
      showPanel(context, parsed, textDoc.fileName);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("kprompt.approvePlanViaCLI", async () => {
      const doc =
        lastDoc ??
        (vscode.window.activeTextEditor
          ? parsePlanResult(vscode.window.activeTextEditor.document.getText())
          : null);
      if (!doc) {
        void vscode.window.showWarningMessage(
          "Open a PlanResult first (kprompt: Open PlanResult)."
        );
        return;
      }
      await approveViaCLI(doc);
    })
  );
}

export function deactivate(): void {
  panel?.dispose();
  panel = undefined;
  lastDoc = null;
}

function showPanel(
  context: vscode.ExtensionContext,
  doc: PlanResultDoc,
  titleHint: string
): void {
  const title = `PlanResult · ${doc.plan?.summary || doc.prompt || "review"}`.slice(
    0,
    80
  );
  if (panel) {
    panel.reveal(vscode.ViewColumn.Beside);
  } else {
    panel = vscode.window.createWebviewPanel(
      "kpromptPlanResult",
      title,
      vscode.ViewColumn.Beside,
      { enableScripts: true, retainContextWhenHidden: true }
    );
    panel.onDidDispose(() => {
      panel = undefined;
    });
    panel.webview.onDidReceiveMessage(async (msg: { type?: string }) => {
      if (msg.type === "dismiss") {
        panel?.dispose();
        return;
      }
      if (msg.type === "approve" && lastDoc) {
        await approveViaCLI(lastDoc);
      }
    });
    context.subscriptions.push(panel);
  }
  panel.title = title;
  const nonce = getNonce();
  panel.webview.html = renderPlanResultHtml(
    doc,
    nonce,
    panel.webview.cspSource
  );
  void titleHint;
}

async function approveViaCLI(doc: PlanResultDoc): Promise<void> {
  if (doc.risk?.denied) {
    void vscode.window.showErrorMessage(
      `Plan is denied: ${doc.risk.message || "hard deny"}`
    );
    return;
  }
  if (doc.applied) {
    void vscode.window.showInformationMessage("This PlanResult is already marked applied.");
    return;
  }
  const prompt = (doc.prompt ?? "").trim();
  if (!prompt) {
    void vscode.window.showErrorMessage(
      "PlanResult has no prompt — cannot rebuild the CLI command."
    );
    return;
  }

  const risk = doc.risk?.level || "unknown";
  const pick = await vscode.window.showWarningMessage(
    `Approve mutating plan (risk=${risk}) via local CLI?\n\n"${prompt}"\n\nThis runs kprompt with --approve in a terminal. Nothing is applied until that process succeeds.`,
    { modal: true },
    "Approve via CLI",
    "Cancel"
  );
  if (pick !== "Approve via CLI") {
    return;
  }

  const cli =
    vscode.workspace.getConfiguration("kprompt").get<string>("cliPath") ||
    "kprompt";
  const ns = doc.plan?.namespace?.trim();
  const shellPrompt = prompt.replace(/'/g, `'\\''`);
  let cmd = `${shellQuote(cli)} '${shellPrompt}' --approve`;
  if (ns) {
    cmd += ` -n ${shellQuote(ns)}`;
  }

  const term = vscode.window.createTerminal({ name: "kprompt approve" });
  term.show(true);
  term.sendText(cmd);
  void vscode.window.showInformationMessage(
    "Started kprompt --approve in a terminal. Review the CLI output before assuming apply succeeded."
  );
}

function shellQuote(s: string): string {
  if (/^[A-Za-z0-9_./:@+=-]+$/.test(s)) {
    return s;
  }
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

function getNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}

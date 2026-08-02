# kprompt VS Code extension (A-076)

Review **PlanResult** JSON in the editor — risk, action diffs, approve via local CLI.

**Not a chat REPL.** The extension never talks to the Kubernetes API and never
auto-applies.

## Install (dev)

```bash
cd ide/vscode
npm install
npm run compile
# then in VS Code: Run → Start Debugging (or install the folder as an extension)
code --install-extension .
```

Or package a VSIX:

```bash
npm run package
code --install-extension kprompt-0.1.0.vsix
```

## Usage

1. Produce a PlanResult: `kprompt "scale api to 3" -n payments -o json > plan.json`
2. Open `plan.json` in VS Code
3. Command Palette → **kprompt: Open PlanResult**
4. Review diffs → **Approve via CLI** (modal confirm) → terminal runs  
   `kprompt '…' --approve -n …`

Setting: `kprompt.cliPath` (default `kprompt`).

## Honesty

- Approve rebuilds the prompt through the **local** CLI; kubeconfig stays on the laptop
- Denied / already-applied plans cannot approve from the webview
- JetBrains / Marketplace publish are follow-ups

## Sample

See [fixtures/scale-api.planresult.json](./fixtures/scale-api.planresult.json).

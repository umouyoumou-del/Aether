// Aether 的 VS Code 插件：启动 `aether lsp`，提供实时诊断、悬停显示类型、跳转到定义、补全与大纲。
// 为了不依赖任何 npm 包，这里直接实现了所需的那一小部分语言服务器协议（JSON-RPC over stdio）。
'use strict';

const vscode = require('vscode');
const cp = require('child_process');
const fs = require('fs');
const path = require('path');

const SELECTOR = { language: 'aether' };

class LspClient {
  constructor(command, output, onExit) {
    this.output = output;
    this.buf = Buffer.alloc(0);
    this.nextId = 1;
    this.pending = new Map();
    this.handlers = new Map();
    this.alive = true;
    this.proc = cp.spawn(command, ['lsp'], { stdio: ['pipe', 'pipe', 'pipe'] });
    this.proc.stdout.on('data', (d) => this.onData(d));
    this.proc.stderr.on('data', (d) => output.append(d.toString()));
    this.proc.on('error', (e) => {
      this.alive = false;
      vscode.window.showErrorMessage(
        `Aether：无法启动 “${command} lsp”（${e.message}）。请把 aether 加入 PATH，或在设置中指定 aether.path。`);
    });
    this.proc.on('exit', (code) => {
      this.alive = false;
      for (const p of this.pending.values()) p.resolve(null);
      this.pending.clear();
      output.appendLine(`aether lsp 已退出（${code}）`);
      if (onExit) onExit(code);
    });
  }

  onData(d) {
    this.buf = Buffer.concat([this.buf, d]);
    for (;;) {
      const sep = this.buf.indexOf('\r\n\r\n');
      if (sep < 0) return;
      const header = this.buf.slice(0, sep).toString('ascii');
      const m = /Content-Length:\s*(\d+)/i.exec(header);
      if (!m) {
        this.buf = this.buf.slice(sep + 4);
        continue;
      }
      const len = parseInt(m[1], 10);
      if (this.buf.length < sep + 4 + len) return;
      const body = this.buf.slice(sep + 4, sep + 4 + len).toString('utf8');
      this.buf = this.buf.slice(sep + 4 + len);
      let msg;
      try {
        msg = JSON.parse(body);
      } catch (e) {
        continue;
      }
      if (msg.method === undefined && msg.id !== undefined) {
        const p = this.pending.get(msg.id);
        if (p) {
          this.pending.delete(msg.id);
          if (msg.error) {
            this.output.appendLine(`请求失败：${msg.error.message}`);
            p.resolve(null);
          } else {
            p.resolve(msg.result);
          }
        }
      } else if (msg.method) {
        const h = this.handlers.get(msg.method);
        if (h) h(msg.params);
        if (msg.id !== undefined) this.write({ jsonrpc: '2.0', id: msg.id, result: null });
      }
    }
  }

  write(msg) {
    if (!this.alive) return;
    const body = Buffer.from(JSON.stringify(msg), 'utf8');
    this.proc.stdin.write(`Content-Length: ${body.length}\r\n\r\n`);
    this.proc.stdin.write(body);
  }

  request(method, params, token) {
    if (!this.alive) return Promise.resolve(null);
    const id = this.nextId++;
    this.write({ jsonrpc: '2.0', id, method, params });
    return new Promise((resolve) => {
      this.pending.set(id, { resolve });
      if (token) {
        token.onCancellationRequested(() => {
          this.write({ jsonrpc: '2.0', method: '$/cancelRequest', params: { id } });
        });
      }
    });
  }

  notify(method, params) {
    this.write({ jsonrpc: '2.0', method, params });
  }

  stop() {
    if (!this.alive) return;
    this.request('shutdown', null).then(() => {
      this.notify('exit', null);
      setTimeout(() => this.proc.kill(), 500);
    });
  }
}

function findAether() {
  const configured = vscode.workspace.getConfiguration('aether').get('path');
  if (configured) return configured;
  const exe = process.platform === 'win32' ? 'aether.exe' : 'aether';
  for (const folder of vscode.workspace.workspaceFolders || []) {
    const candidate = path.join(folder.uri.fsPath, 'bin', exe);
    if (fs.existsSync(candidate)) return candidate;
  }
  return 'aether';
}

const toRange = (r) => new vscode.Range(r.start.line, r.start.character, r.end.line, r.end.character);
const docPos = (doc, pos) => ({ textDocument: { uri: doc.uri.toString() }, position: { line: pos.line, character: pos.character } });

function toSymbol(s) {
  // LSP 的 SymbolKind 从 1 开始，VS Code 的从 0 开始
  const sym = new vscode.DocumentSymbol(s.name, s.detail || '', s.kind - 1, toRange(s.range), toRange(s.selectionRange));
  sym.children = (s.children || []).map(toSymbol);
  return sym;
}

let client = null;

function start(context, diags, output) {
  client = new LspClient(findAether(), output);
  client.handlers.set('textDocument/publishDiagnostics', (p) => {
    const uri = vscode.Uri.parse(p.uri);
    diags.set(uri, p.diagnostics.map((d) => {
      const severity = d.severity === 1 ? vscode.DiagnosticSeverity.Error
        : d.severity === 2 ? vscode.DiagnosticSeverity.Warning : vscode.DiagnosticSeverity.Information;
      const diag = new vscode.Diagnostic(toRange(d.range), d.message, severity);
      diag.source = d.source;
      return diag;
    }));
  });
  const c = client;
  const workspace = (vscode.workspace.workspaceFolders || [])[0];
  c.request('initialize', {
    processId: process.pid,
    rootUri: workspace ? workspace.uri.toString() : null,
    capabilities: {},
  }).then(() => {
    c.notify('initialized', {});
    vscode.workspace.textDocuments.forEach(didOpen);
  });
}

function didOpen(doc) {
  if (!client || doc.languageId !== 'aether') return;
  client.notify('textDocument/didOpen', {
    textDocument: { uri: doc.uri.toString(), languageId: 'aether', version: doc.version, text: doc.getText() },
  });
}

function activate(context) {
  const output = vscode.window.createOutputChannel('Aether');
  const diags = vscode.languages.createDiagnosticCollection('aether');
  start(context, diags, output);

  const isAether = (doc) => doc.languageId === 'aether';
  context.subscriptions.push(
    output,
    diags,
    vscode.workspace.onDidOpenTextDocument(didOpen),
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (!client || !isAether(e.document) || e.contentChanges.length === 0) return;
      client.notify('textDocument/didChange', {
        textDocument: { uri: e.document.uri.toString(), version: e.document.version },
        contentChanges: [{ text: e.document.getText() }],
      });
    }),
    vscode.workspace.onDidSaveTextDocument((doc) => {
      if (client && isAether(doc)) client.notify('textDocument/didSave', { textDocument: { uri: doc.uri.toString() } });
    }),
    vscode.workspace.onDidCloseTextDocument((doc) => {
      if (!client || !isAether(doc)) return;
      client.notify('textDocument/didClose', { textDocument: { uri: doc.uri.toString() } });
      diags.delete(doc.uri);
    }),
    vscode.languages.registerHoverProvider(SELECTOR, {
      async provideHover(doc, pos, token) {
        const r = client && await client.request('textDocument/hover', docPos(doc, pos), token);
        return r ? new vscode.Hover(new vscode.MarkdownString(r.contents.value)) : null;
      },
    }),
    vscode.languages.registerDefinitionProvider(SELECTOR, {
      async provideDefinition(doc, pos, token) {
        const r = client && await client.request('textDocument/definition', docPos(doc, pos), token);
        return r ? new vscode.Location(vscode.Uri.parse(r.uri), toRange(r.range)) : null;
      },
    }),
    vscode.languages.registerDocumentSymbolProvider(SELECTOR, {
      async provideDocumentSymbols(doc, token) {
        const r = client && await client.request('textDocument/documentSymbol', { textDocument: { uri: doc.uri.toString() } }, token);
        return (r || []).map(toSymbol);
      },
    }),
    vscode.languages.registerCompletionItemProvider(SELECTOR, {
      async provideCompletionItems(doc, pos, token) {
        const r = client && await client.request('textDocument/completion', docPos(doc, pos), token);
        return (r || []).map((c) => {
          // LSP 的 CompletionItemKind 从 1 开始，VS Code 的从 0 开始
          const item = new vscode.CompletionItem(c.label, (c.kind || 1) - 1);
          item.detail = c.detail;
          return item;
        });
      },
    }, '.'),
    vscode.commands.registerCommand('aether.restartServer', () => {
      if (client) client.stop();
      diags.clear();
      start(context, diags, output);
    }),
    { dispose: () => client && client.stop() },
  );
}

function deactivate() {
  if (client) client.stop();
}

module.exports = { activate, deactivate };

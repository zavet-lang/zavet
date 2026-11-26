import * as vscode from 'vscode';
import { execFile } from 'child_process';
import { LanguageClient, LanguageClientOptions, ServerOptions } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

export function activate(context: vscode.ExtensionContext) {
    const config = vscode.workspace.getConfiguration('zavet');
    const zavetPath = config.get<string>('executablePath', 'zavet');

    // LSP опционален: подсветка работает и без бинарника
    try {
        const serverOptions: ServerOptions = {
            command: zavetPath,
            args: ['lsp'],
        };
        const clientOptions: LanguageClientOptions = {
            documentSelector: [{ scheme: 'file', language: 'zavet' }],
        };
        client = new LanguageClient('zavet', 'Завет Language Server', serverOptions, clientOptions);
        client.start().catch(() => {
            // бинарник zavet недоступен — только подсветка/сниппеты
            client = undefined;
        });
    } catch {
        client = undefined;
    }

    context.subscriptions.push(
        vscode.commands.registerCommand('zavet.run', () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor) {
                vscode.window.showErrorMessage('Нет открытого файла');
                return;
            }
            if (editor.document.languageId !== 'zavet') {
                vscode.window.showErrorMessage('Откройте файл .молитва');
                return;
            }
            editor.document.save().then(() => runFile(editor.document.fileName));
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('zavet.runInTerminal', () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || editor.document.languageId !== 'zavet') {
                vscode.window.showErrorMessage('Откройте файл .молитва');
                return;
            }
            editor.document.save().then(() => runInTerminal(editor.document.fileName));
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('zavet.openREPL', () => openREPL())
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('zavet.format', () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || editor.document.languageId !== 'zavet') {
                vscode.window.showErrorMessage('Откройте файл .молитва');
                return;
            }
            vscode.commands.executeCommand('editor.action.formatDocument');
        })
    );
}

function zavetExecutable(): string {
    return vscode.workspace.getConfiguration('zavet').get<string>('executablePath', 'zavet')!;
}

function runFile(filePath: string) {
    const output = vscode.window.createOutputChannel('Завет');
    output.clear();
    output.show(true);
    execFile(zavetExecutable(), [filePath], { encoding: 'utf8' }, (error, stdout, stderr) => {
        if (stdout) {
            output.appendLine(stdout);
        }
        if (stderr) {
            output.appendLine(stderr);
        }
        if (error && !stderr) {
            output.appendLine(`Ошибка: ${error.message}`);
        }
    });
}

function runInTerminal(filePath: string) {
    const terminal = vscode.window.createTerminal('Завет');
    terminal.show();
    terminal.sendText(`"${zavetExecutable()}" "${filePath}"`);
}

function openREPL() {
    const terminal = vscode.window.createTerminal('Завет REPL');
    terminal.show();
    terminal.sendText(`"${zavetExecutable()}"`);
}

export function deactivate(): Thenable<void> | undefined {
    return client?.stop();
}

// The harness MCP host an mcpApp surface's generated Playwright specs run in (FPC plan 5, D11).
//
// It does what a real MCP Apps host does, in the smallest form that proves the app works there:
// connect an MCP client to the launched app's /mcp (reverse-proxied onto this page's own origin, so
// no CORS is involved), find the tool whose _meta.ui.resourceUri names the surface, read that
// resource, render it in a sandboxed iframe and bridge it, so the app's own tool calls
// (App.callServerTool) reach the server through the host. Written after ext-apps'
// examples/basic-host. Nothing here is specific to one app.
//
// The page names the surface (<body data-surface>) and the screen to open (?route=): the route is
// set as the app's location.hash before the app's script runs, since an MCP App has no address bar.
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { CallToolResultSchema } from '@modelcontextprotocol/sdk/types.js';
import { AppBridge,PostMessageTransport, getToolUiResourceUri } from '@modelcontextprotocol/ext-apps/app-bridge';

/** The MIME type of an MCP App's ui:// resource (ext-apps' RESOURCE_MIME_TYPE). */
const RESOURCE_MIME_TYPE = 'text/html;profile=mcp-app';

function fail(message: string): never {
  document.body.dataset['error'] = message;
  throw new Error(message);
}

/** html with a script that sets the app's location.hash to route, ahead of everything the app runs. */
function withRoute(html: string, route: string): string {
  const setHash = `<script>location.hash = ${JSON.stringify(route).replace(/</g, '\\u003c')};</script>`;
  const head = /<head[^>]*>/i.exec(html);
  if (head === null) return setHash + html;
  const at = head.index + head[0].length;
  return html.slice(0, at) + setHash + html.slice(at);
}

async function main(): Promise<void> {
  const surface = document.body.dataset['surface'] ?? '';
  if (surface === '') fail('the host page names no surface (<body data-surface>)');
  const route = new URLSearchParams(location.search).get('route') ?? '/';
  const iframe = document.getElementById('mcp-app');
  if (!(iframe instanceof HTMLIFrameElement) || iframe.contentWindow === null) fail('the host page has no #mcp-app iframe');

  const client = new Client({ name: 'aiarch-scenario-host', version: '0' });
  await client.connect(new StreamableHTTPClientTransport(new URL('/mcp', location.origin)));

  // The tool that names the surface's resource: the first of tools/list, which is in a stable order.
  const { tools } = await client.listTools();
  const tool = tools.find((t) => (getToolUiResourceUri(t) ?? '').includes(`/${surface}/`));
  const uri = tool === undefined ? undefined : getToolUiResourceUri(tool);
  if (tool === undefined || uri === undefined) fail(`no tool names a ui:// resource of the surface ${surface}`);

  // A host calls the tool the resource is attached to and hands the app its input and result. A tool
  // that is not read-only is not called: the host must not change the app's state by opening it.
  const toolArguments: Record<string, unknown> = {};
  const called = tool.annotations?.readOnlyHint === true ? await client.callTool({ name: tool.name, arguments: toolArguments }) : undefined;
  const parsed = called === undefined ? undefined : CallToolResultSchema.safeParse(called);
  const toolResult = parsed?.success === true ? parsed.data : undefined;

  const resource = await client.readResource({ uri });
  const content = resource.contents[0];
  if (content === undefined || !('text' in content) || typeof content.text !== 'string') fail(`${uri} has no text content`);
  if (content.mimeType !== RESOURCE_MIME_TYPE) fail(`${uri} is served as ${String(content.mimeType)}, want ${RESOURCE_MIME_TYPE}`);

  // The bridge listens before the app's document exists: the app's first message (ui/initialize) is
  // sent as its script runs, and one sent before the host listens is lost.
  const bridge = new AppBridge(client, { name: 'aiarch-scenario-host', version: '0' }, { serverTools: {}, openLinks: {} });
  bridge.oninitialized = () => {
    void bridge.sendToolInput({ arguments: toolArguments });
    if (toolResult !== undefined) void bridge.sendToolResult(toolResult);
  };
  await bridge.connect(new PostMessageTransport(iframe.contentWindow, iframe.contentWindow));
  iframe.srcdoc = withRoute(content.text, route);
}

main().catch((err: unknown) => {
  console.error('mcp app host:', err);
  document.body.dataset['error'] ??= String(err);
});

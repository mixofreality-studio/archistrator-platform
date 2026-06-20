import {
  BatchSpanProcessor,
  WebTracerProvider,
} from '@opentelemetry/sdk-trace-web';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { ATTR_SERVICE_NAME } from '@opentelemetry/semantic-conventions';
import {
  LoggerProvider,
  BatchLogRecordProcessor,
} from '@opentelemetry/sdk-logs';
import { OTLPLogExporter } from '@opentelemetry/exporter-logs-otlp-http';

/**
 * Initialises OpenTelemetry tracing and logging for a web application.
 *
 * Call this once at app startup (e.g. from main.tsx / index.tsx).
 * Telemetry is only active when `mode === 'production'`; in dev/test the
 * function returns immediately to avoid failed OTLP export calls.
 *
 * @param serviceName - The OTel service.name attribute (e.g. 'archistrator-webapp').
 * @param mode        - The build mode string; pass `import.meta.env.MODE` from
 *                      the consuming app. Defaults to `'development'`.
 */
export function setupTelemetry(
  serviceName: string,
  mode: string = 'development',
): void {
  if (mode !== 'production') {
    return;
  }

  const resource = resourceFromAttributes({
    [ATTR_SERVICE_NAME]: serviceName,
    'deployment.environment.name': mode,
  });

  // --- Traces ---
  const traceExporter = new OTLPTraceExporter({
    url: '/otlp/v1/traces',
  });

  const tracerProvider = new WebTracerProvider({
    resource,
    spanProcessors: [new BatchSpanProcessor(traceExporter)],
  });

  tracerProvider.register();

  registerInstrumentations({
    instrumentations: [
      new FetchInstrumentation({
        ignoreUrls: [/\/otlp\//],
      }),
      new DocumentLoadInstrumentation(),
    ],
  });

  // --- Logs ---
  const logExporter = new OTLPLogExporter({
    url: '/otlp/v1/logs',
  });

  const loggerProvider = new LoggerProvider({
    resource,
    processors: [new BatchLogRecordProcessor(logExporter)],
  });

  const logger = loggerProvider.getLogger(serviceName);

  // --- Global error capture ---
  window.addEventListener('error', (event) => {
    const err: unknown = event.error;
    logger.emit({
      severityText: 'ERROR',
      body: event.message,
      attributes: {
        'exception.type': err instanceof Error ? err.name : 'Error',
        'exception.message': event.message,
        'exception.stacktrace': err instanceof Error ? (err.stack ?? '') : '',
        'code.filepath': event.filename,
        'code.lineno': event.lineno,
        'code.column': event.colno,
      },
    });
  });

  window.addEventListener('unhandledrejection', (event) => {
    const reason: unknown = event.reason;
    const message =
      reason instanceof Error ? reason.message : String(reason);
    logger.emit({
      severityText: 'ERROR',
      body: `Unhandled promise rejection: ${message}`,
      attributes: {
        'exception.type':
          reason instanceof Error ? reason.name : 'UnhandledRejection',
        'exception.message': message,
        'exception.stacktrace':
          reason instanceof Error ? (reason.stack ?? '') : '',
      },
    });
  });

  // --- Fetch error logging ---
  const originalFetch = window.fetch;
  window.fetch = async (...args: Parameters<typeof fetch>): Promise<Response> => {
    const url =
      args[0] instanceof Request ? args[0].url : String(args[0]);

    // Don't log telemetry export failures (avoid recursion)
    if (url.includes('/otlp/')) {
      return originalFetch(...args);
    }

    const callsite = new Error();
    try {
      const response = await originalFetch(...args);
      if (!response.ok) {
        logger.emit({
          severityText: 'ERROR',
          body: `HTTP ${String(response.status)} ${response.statusText} — ${args[1]?.method ?? 'GET'} ${url}`,
          attributes: {
            'http.request.method': args[1]?.method ?? 'GET',
            'url.full': url,
            'http.response.status_code': response.status,
            'exception.stacktrace': callsite.stack ?? '',
          },
        });
      }
      return response;
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      logger.emit({
        severityText: 'ERROR',
        body: `Fetch failed — ${args[1]?.method ?? 'GET'} ${url}: ${error.message}`,
        attributes: {
          'http.request.method': args[1]?.method ?? 'GET',
          'url.full': url,
          'exception.type': error.name,
          'exception.message': error.message,
          'exception.stacktrace': callsite.stack ?? '',
        },
      });
      throw err;
    }
  };

  // Flush buffered telemetry when user navigates away
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      void tracerProvider.forceFlush();
      void loggerProvider.forceFlush();
    }
  });
}

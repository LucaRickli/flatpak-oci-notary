import { Code, ConnectError, type Interceptor, type Transport } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';

type UnauthenticatedListener = () => void;
const unauthenticatedListeners = new Set<UnauthenticatedListener>();

/** Registers a callback invoked whenever an RPC fails with Code.Unauthenticated. */
export function onUnauthenticated(fn: UnauthenticatedListener): () => void {
	unauthenticatedListeners.add(fn);
	return () => unauthenticatedListeners.delete(fn);
}

const authInterceptor: Interceptor = (next) => async (req) => {
	try {
		return await next(req);
	} catch (err) {
		if (ConnectError.from(err).code === Code.Unauthenticated) {
			for (const fn of unauthenticatedListeners) fn();
		}
		throw err;
	}
};

/** Connect transport for the admin API, served by the Go server under /api. */
export const transport: Transport = createConnectTransport({
	baseUrl: '/api',
	useBinaryFormat: true,
	interceptors: [authInterceptor],
	fetch: (input, init) => fetch(input, { ...init, credentials: 'same-origin' })
});

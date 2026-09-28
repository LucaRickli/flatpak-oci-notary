import { toast } from 'svelte-sonner';
import { Code, ConnectError, errorMessage, type GetInfoResponse, type GetMeResponse } from '$lib/api';

/** Global session state, filled by the root layout. Mutate in place only. */
export const session = $state<{
	me: GetMeResponse | undefined;
	info: GetInfoResponse | undefined;
	/** Set when any RPC failed with Code.Unauthenticated (e.g. the session cookie expired). */
	expired: boolean;
}>({ me: undefined, info: undefined, expired: false });

export function isUnauthenticated(err: unknown): boolean {
	return ConnectError.from(err).code === Code.Unauthenticated;
}

export function isNotFound(err: unknown): boolean {
	return ConnectError.from(err).code === Code.NotFound;
}

/**
 * Shows an error toast for a failed RPC. Unauthenticated errors are skipped:
 * the transport interceptor already flips the app to the sign-in screen.
 */
export function reportError(err: unknown, title = 'Something went wrong') {
	if (isUnauthenticated(err)) return;
	toast.error(title, { description: errorMessage(err) });
}

import { ConnectError } from '@connectrpc/connect';

/** Human-readable message for any error thrown by an RPC. */
export function errorMessage(err: unknown): string {
	return ConnectError.from(err).rawMessage;
}

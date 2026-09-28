import { createClient, type Client } from '@connectrpc/connect';
import {
	ImageService,
	RegistryService,
	RepositoryService,
	SystemService
} from './gen/notary/v1/notary_pb';
import { transport } from './transport';

/** Server info, session and dashboard overview. */
export const systemClient: Client<typeof SystemService> = createClient(SystemService, transport);

/** Upstream OCI registries. */
export const registryClient: Client<typeof RegistryService> = createClient(
	RegistryService,
	transport
);

/** Indexed flatpak images. */
export const imageClient: Client<typeof ImageService> = createClient(ImageService, transport);

/** Published flatpak repositories. */
export const repositoryClient: Client<typeof RepositoryService> = createClient(
	RepositoryService,
	transport
);

/** Plain HTTP endpoints of the Go server that are not RPCs. */
export const urls = {
	/** PNG icon of an image (when Image.hasIcon). */
	icon: (imageId: number) => `/icons/${imageId}`,
	/** Starts the OIDC login flow and returns to `redirect` afterwards. */
	login: (redirect = location.pathname + location.search) =>
		`/auth/login?${new URLSearchParams({ redirect })}`
};

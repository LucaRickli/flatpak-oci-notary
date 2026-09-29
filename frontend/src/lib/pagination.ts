/** Page sizes offered by tables. The server clamps page_size to 500. */
export const PAGE_SIZES = [25, 50, 100, 200];

/** Same as the server's default for page_size 0. */
export const DEFAULT_PAGE_SIZE = 50;

/** Largest offset the API accepts (offset is an int32). */
const MAX_OFFSET = 2 ** 31 - 1;

/** Highest 1-based page whose offset still fits the API's int32 offset. */
export function maxPage(pageSize: number) {
	return Math.floor(MAX_OFFSET / pageSize) + 1;
}

/** Request fields (pageSize, offset) for a 1-based page. */
export function pageRequest(page: number, pageSize: number) {
	return { pageSize, offset: Math.min(Math.max(0, (page - 1) * pageSize), MAX_OFFSET) };
}

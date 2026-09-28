import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backend = 'http://localhost:8080';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) => filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// Pure SPA: the Go server embeds `build/` and serves index.html as the fallback.
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html'
			})
		})
	],
	server: {
		proxy: {
			'/api': backend,
			'/auth': backend,
			'/icons': backend,
			// Trailing slash: a bare '/repo' prefix would also swallow the SPA's /repositories routes.
			'/repo/': backend,
			'/healthz': backend
		}
	}
});

import { build } from 'esbuild';

await build({
  entryPoints: ['src/main.js'],
  bundle: true,
  format: 'esm',
  target: ['es2022'],
  outfile: 'dist/viewer.js',
  loader: { '.css': 'text' },
  sourcemap: 'linked',
  logLevel: 'info'
});

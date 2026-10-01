# Bundled browser dependencies

`internal/webapp/static/markdown-it.min.js` is the browser bundle from
`markdown-it` 14.3.2, published at https://www.npmjs.com/package/markdown-it.
It is served locally so Markdown works in air-gapped deployments. The upstream
MIT license is preserved in `markdown-it.LICENSE`. License notices for its
packaged dependencies are preserved in `markdown-it-deps/` (argparse, entities,
linkify-it, mdurl, punycode.js, and uc.micro). The shipped bundle's SHA-256 is
`e32488403e2e565ac12a9669bfdf2b1b876eb0a5c84f8e0699884b562d18eb52`.

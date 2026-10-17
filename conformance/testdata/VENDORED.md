# Vendored conformance fixtures

Do not edit anything in `fixtures/`. It is a copy of
`packages/avsb-conformance/fixtures/`, produced by
`npm run vendor` in that package, so the published repo carries the
corpus its conformance suite needs.

Engine that generated this corpus: @avsbhq/core 1.7.0

Why here: Go excludes testdata directories from builds by convention.

To refresh: run `npm run generate` then `npm run vendor` in
packages/avsb-conformance and commit the result.

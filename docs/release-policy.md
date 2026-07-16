# Protocol release policy

HAP protocol releases use Semantic Versioning and immutable Git tags.

Release candidates use tags such as `v0.2.0-rc.1`. A candidate contains:

- all versioned schemas;
- conformance fixtures and scenarios;
- `SHA256SUMS`; and
- the current specification, examples, migration guide, and changelog.

Consumers pin both the release tag and checksum. If integration discovers a
defect, maintainers publish a new candidate rather than replacing an existing
tag or asset.

A final release requires:

1. protocol tests and release verification passing from a clean clone;
2. CLI and SDK consumers passing against the immutable candidate;
3. reference agents passing their declared interface conformance;
4. marketplace and website consumers using byte-identical schemas; and
5. explicit release authorization.

Protocol, CLI, SDK, agents, marketplace, and website version independently.

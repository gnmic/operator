# Code and Documentation Generation

The API interface is specified as an OpenAPI contract in the file `openapi.yaml`. This contract allows for manual code and documentation generation.

## Code Generation

To generate the code, install [openapi-codegen](https://github.com/oapi-codegen/oapi-codegen). Then run `go generate ./internal/apiserver` to generate the `gen.go` file. The `cfg.yaml` file specifies the file path.

## Documentation Generation

The generated documentation can be found under `docs/content/docs/advanced/`. It uses Mustache files (`.../advanced/openapi-templates`) as templates to add the required header for rendering and ensure that the links are valid.

To regenerate the documentation, run from the root folder of this repository: `docker run --rm --user "$(id -u):$(id -g)" -v ${PWD}:/local openapitools/openapi-generator-cli generate -c /local/docs/content/docs/advanced/openapi-templates/openapi-generator-config.yaml`.

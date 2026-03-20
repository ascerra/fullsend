.DEFAULT_GOAL := help
.PHONY: help lint lint-adr-status lint-adr-numbers lint-adr-frontmatter

help:
	@echo "Available targets:"
	@echo "  help                - Show this help message"
	@echo "  lint                - Run all linting and validation"
	@echo "  lint-adr-status     - Validate ADR statuses in all ADR files"
	@echo "  lint-adr-numbers    - Check for duplicate ADR numeric identifiers"
	@echo "  lint-adr-frontmatter - Validate ADR frontmatter and cross-references"

lint: lint-adr-status lint-adr-numbers lint-adr-frontmatter

lint-adr-status:
	@./hack/lint-adr-status

lint-adr-numbers:
	@./hack/lint-adr-numbers

lint-adr-frontmatter:
	@python3 ./hack/lint-adr-frontmatter

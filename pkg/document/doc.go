// Package document implements the MOCA document runtime engine.
//
// Every record in MOCA is a Document — a typed, lifecycle-managed entity
// backed by a MetaType definition. This package provides the Document interface,
// the DynamicDoc implementation, and the full document lifecycle (before_insert,
// after_insert, before_save, after_save, before_submit, on_submit, etc.).
//
// Key components:
//   - Document: interface + DynamicDoc struct
//   - Lifecycle: hook invocation at each lifecycle stage
//   - Naming: naming strategy resolution (autoname, by field, series)
//   - Validator: field-level and cross-field validation
//   - Controller: controller resolution and composition for custom behavior
package document

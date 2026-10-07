# odx-verify

A small command-line sanity checker for ODX 2.0.1 and 2.2.0 files, written in Go.

Point it at an `.odx` (or `.pdx`) file and it prints `OK!` when nothing's wrong, or a line-by-line list of the things it noticed.

The target is **Vector ODX Studio 9**: where this checker has to pick a behaviour, it tries to match Vector's reader for ODX 2.0.1 and 2.2.0. Where another tool and Vector disagree on what is acceptable, Vector wins.

**Please read this as working notes, not a spec.**
Everything below is best-effort attempt at understanding ODX 2.0.1 / 2.2.0 from looking at files and comparing results. Use it as a starting point and check against your own tooling before trusting it.

---

## Build and run

```sh
go build -o odx-verify .
./odx-verify path/to/file.odx
./odx-verify path/to/file.pdx
```

- `.odx` — a single XML document.
- `.pdx` — a zip archive; every `.odx` / `.xml` entry inside is checked.

The version is picked from the root `MODEL-VERSION` attribute (`2.0.1` or `2.2.0`). Anything else is reported as unsupported.

Exit codes: `0` = `OK!`, `1` = something looked wrong, `2` = usage / I/O error.

Example failure output:

```
NOT OK:
  line 12: missing required element <PHYSICAL-TYPE> in <DTC-DOP>
  line 18: invalid value "A_ASCIISTRING" for attribute BASE-DATA-TYPE on <PHYSICAL-TYPE> (allowed: A_INT32, ...)
```

---

## What it currently checks

These are the things enforced.

- The document shape: `<ODX MODEL-VERSION="2.2.0">` with exactly one top-level category.
- Element order (children have to appear in a particular sequence), so the checker enforces an order rather than treating elements as interchangeable.
- Required vs. optional children, and required attributes.
- Attribute and element enumerations, patterns and simple numeric ranges (e.g. `SHORT-NAME`, `BIT-LENGTH`, `BIT-POSITION`).
- Unique `ID` values across the document.
- `xsi:type` on the element kinds that need it and only when it names a known type that is the declared type or derives from it (see below).
- Rich text in `<DESC>` (XHTML mixed content, e.g. a `<p>` holding text and inline tags).

It does **not** try to do anything semantic. It won't tell you whether a reference actually points somewhere sensible, only whether the document is
shaped correctly.

---

## ODX 2.0.1 / 2.2.0

### Document shape

```xml
<ODX MODEL-VERSION="2.2.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <!-- exactly one of: -->
  <DIAG-LAYER-CONTAINER> ... </DIAG-LAYER-CONTAINER>
  <!-- or COMPARAM-SPEC / VEHICLE-INFO-SPEC / FLASH / ECU-CONFIG /
       MULTIPLE-ECU-JOB-SPEC / COMPARAM-SUBSET / FUNCTION-DICTIONARY -->
</ODX>
```

`MODEL-VERSION` is `2.0.1` or `2.2.0` (the two versions I've seen in the wild). Diagnostic files use `DIAG-LAYER-CONTAINER`. `COMPARAM-SUBSET` (ODX-CS) is accepted as a root in 2.2.0, but a `COMPARAM-SPEC` root is flagged: Vector doesn't take it as a root, even though the schema technically permits it. 2.0.1 has a smaller set of top-level categories (no `ECU-CONFIG`, `COMPARAM-SUBSET` or `FUNCTION-DICTIONARY`).

### Order matters

The children of each element are an ordered sequence.
For example:

- Inside `DIAG-DATA-DICTIONARY-SPEC`, `DTC-DOPS` appears **before** `DATA-OBJECT-PROPS`.
- Inside `DTC-DOP`, the order looks like `DIAG-CODED-TYPE → PHYSICAL-TYPE → COMPU-METHOD → DTCS`, with `COMPU-METHOD` required and before `DTCS`.

### References

Three kinds show up: `...-REF` (cross-reference by ID), `...-SNREF`
(short-name reference) and `...-SNPATHREF` (dotted path, 2.2.0 only; 2.0.1
has only the first two). The checker only validates their shape, not whether
they resolve.

### Names and IDs

- `SHORT-NAME` looks like `[a-zA-Z0-9_]{1,128}` in 2.2.0; in 2.0.1 it is a plain string with no pattern.
- `ID` values appear to need to be unique across the file.
- `LONG-NAME` seems capped around 255 characters.

---

## Files

```
main.go        CLI entry point and .pdx handling
validate.go    XML parsing + top-level checks
schema.go      loads the ODX schemas and builds the rule model
engine.go      the actual validation walk
odx220.xsd     ODX 2.2.0 schema the checker validates against
odx220-xhtml.xsd  rich-text subset used by <DESC>
odx201.xsd     ODX 2.0.1 schema
odx201-xhtml.xsd  rich-text subset for 2.0.1
testdata/      fixtures: minimal docs, deliberately broken docs, and real
               ODX-CS comparam files under testdata/odxtools/
```

## Tests

```sh
go test ./...
```

The suite includes the minimal documents, the real ODX-CS comparam files under `testdata/odxtools/`, a pile of deliberately broken documents covering each rule above, and some `.pdx` round-trips.

The comparam examples under `testdata/odxtools/` are taken from the [mercedes-benz/odxtools](https://github.com/mercedes-benz/odxtools) project (MIT licensed), used here as an external real-world corpus. They are checked against the same rules as everything else. Vector wins where odxtools and Vector
disagree.

## Future development

If this cli disagrees with Vector, Vector is right and this verifier should adjust. Open an issue with the file when any discrepancy is found.

Maintained by [ucukertz](https://github.com/ucukertz).

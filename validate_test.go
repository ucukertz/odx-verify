package main

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// variant wraps ECU-VARIANT sections in a minimal document that the target
// parser accepts (xsi:type polymorphism).
func variant(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<ODX MODEL-VERSION="2.2.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="odx.xsd">
  <DIAG-LAYER-CONTAINER ID="C">
    <SHORT-NAME>C</SHORT-NAME>
    <ECU-VARIANTS>
      <ECU-VARIANT ID="E">
        <SHORT-NAME>E</SHORT-NAME>
` + inner + `
      </ECU-VARIANT>
    </ECU-VARIANTS>
  </DIAG-LAYER-CONTAINER>
</ODX>`
}

// variant201 is the same shape with MODEL-VERSION 2.0.1.
func variant201(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<ODX MODEL-VERSION="2.0.1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="odx.xsd">
  <DIAG-LAYER-CONTAINER ID="C">
    <SHORT-NAME>C</SHORT-NAME>
    <ECU-VARIANTS>
      <ECU-VARIANT ID="E">
        <SHORT-NAME>E</SHORT-NAME>
` + inner + `
      </ECU-VARIANT>
    </ECU-VARIANTS>
  </DIAG-LAYER-CONTAINER>
</ODX>`
}

// valueParam builds a REQUEST holding one VALUE param whose DOP reference is
// given as raw XML (e.g. <DOP-SNREF SHORT-NAME="Dop"/>), for testing SNREF.
func valueParam(dopRef string) string {
	return `<REQUESTS><REQUEST ID="R"><SHORT-NAME>R</SHORT-NAME><PARAMS>` +
		`<PARAM xsi:type="VALUE"><SHORT-NAME>V</SHORT-NAME>` + dopRef + `</PARAM>` +
		`</PARAMS></REQUEST></REQUESTS>`
}

// envDataDesc builds a DIAG-DATA-DICTIONARY-SPEC holding one ENV-DATA-DESC
// whose scope reference is given as raw XML, for testing SNPATHREF.
func envDataDesc(ref string) string {
	return `<DIAG-DATA-DICTIONARY-SPEC><ENV-DATA-DESCS><ENV-DATA-DESC ID="ED"><SHORT-NAME>ED</SHORT-NAME>` +
		ref + `<ENV-DATA-REFS><ENV-DATA-REF ID-REF="X"/></ENV-DATA-REFS></ENV-DATA-DESC></ENV-DATA-DESCS></DIAG-DATA-DICTIONARY-SPEC>`
}

const dtcDopValid = `<DIAG-DATA-DICTIONARY-SPEC><DTC-DOPS><DTC-DOP ID="X"><SHORT-NAME>X</SHORT-NAME>` +
	`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"><BIT-LENGTH>8</BIT-LENGTH></DIAG-CODED-TYPE>` +
	`<PHYSICAL-TYPE BASE-DATA-TYPE="A_UINT32"/>` +
	`<COMPU-METHOD><CATEGORY>IDENTICAL</CATEGORY></COMPU-METHOD>` +
	`<DTCS><DTC ID="T"><SHORT-NAME>T</SHORT-NAME><TROUBLE-CODE>1</TROUBLE-CODE><TEXT>x</TEXT></DTC></DTCS>` +
	`</DTC-DOP></DTC-DOPS></DIAG-DATA-DICTIONARY-SPEC>`

func TestValid(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"golden-minimal", mustRead(t, "testdata/minimal.odx")},
		{"golden-minimal-201", mustRead(t, "testdata/minimal-201.odx")},
		{"dtc-dop", variant(dtcDopValid)},
		{"dtc-dop-201", variant201(dtcDopValid)},
		// 2.0.1 has no abstract types: a concrete DIAG-CODED-TYPE needs no xsi:type.
		{"201-concrete-diag-coded-type-no-xsitype", variant201(strings.Replace(dtcDopValid,
			`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"><BIT-LENGTH>8</BIT-LENGTH></DIAG-CODED-TYPE>`,
			`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32"/>`, 1))},
		// 2.0.1 SHORT-NAME is a plain string: spaces are accepted (2.2.0 rejects them).
		{"201-short-name-with-space", variant201(strings.Replace(dtcDopValid,
			`<SHORT-NAME>X</SHORT-NAME>`, `<SHORT-NAME>has space</SHORT-NAME>`, 1))},
		// Rich text (XHTML mixed content) is valid in <DESC>.
		{"desc-rich-220", variant(`<DESC><p>a <b>bold</b> c</p></DESC>`)},
		{"desc-rich-lists-220", variant(`<DESC><p>x</p><p><ul><li>y</li></ul></p></DESC>`)},
		{"desc-rich-201", variant201(`<DESC><p>a <b>bold</b> c</p></DESC>`)},
		{"desc-rich-lists-201", variant201(`<DESC><p>x</p><p><ul><li>y</li></ul></p></DESC>`)},
		// References: SNREF by short-name, SNPATHREF by dotted path (2.2.0).
		{"snref-value-220", variant(valueParam(`<DOP-SNREF SHORT-NAME="Dop"/>`))},
		{"snref-value-201-plain-name", variant201(valueParam(`<DOP-SNREF SHORT-NAME="has space"/>`))},
		{"snpathref-env-data-220", variant(envDataDesc(`<PARAM-SNPATHREF SHORT-NAME-PATH="A.B"/>`))},
		// Real-world ODX 2.2.0 comparam files (ODX-C / ODX-CS).
		{"odxtools-ISO_11898_2_DWCAN", mustRead(t, "testdata/odxtools/ISO_11898_2_DWCAN.odx-cs")},
		{"odxtools-ISO_11898_3_DWFTCAN", mustRead(t, "testdata/odxtools/ISO_11898_3_DWFTCAN.odx-cs")},
		{"odxtools-ISO_15765_2", mustRead(t, "testdata/odxtools/ISO_15765_2.odx-cs")},
		{"odxtools-ISO_15765_3_CPSS", mustRead(t, "testdata/odxtools/ISO_15765_3_CPSS.odx-cs")},
		{"odxtools-SAE_J2411_SWCAN_CPSS", mustRead(t, "testdata/odxtools/SAE_J2411_SWCAN_CPSS.odx-cs")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := validateODX([]byte(tc.doc)); len(errs) != 0 {
				t.Fatalf("expected valid, got errors:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}

func TestInvalid(t *testing.T) {
	base := mustRead(t, "testdata/minimal.odx")
	rep := func(old, new string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("anchor not found in fixture: %q", old)
		}
		return strings.Replace(base, old, new, 1)
	}
	repAll := func(old, new string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("anchor not found in fixture: %q", old)
		}
		return strings.ReplaceAll(base, old, new)
	}

	cases := []struct {
		name string
		doc  string
		want string
	}{
		// ---- document / root ----
		{"bad-model-version", rep(`MODEL-VERSION="2.2.0"`, `MODEL-VERSION="2.3.0"`), "unsupported ODX version"},
		{"missing-model-version", rep(` MODEL-VERSION="2.2.0"`, ``), "missing required attribute MODEL-VERSION"},
		{"root-not-odx", `<DIAG-LAYER-CONTAINER ID="C"><SHORT-NAME>C</SHORT-NAME></DIAG-LAYER-CONTAINER>`, "root element must be <ODX>"},
		{"two-root-categories", `<ODX MODEL-VERSION="2.2.0"><DIAG-LAYER-CONTAINER ID="C"><SHORT-NAME>C</SHORT-NAME></DIAG-LAYER-CONTAINER><FLASH/></ODX>`, "unexpected element <FLASH>"},
		{"no-root-category", `<ODX MODEL-VERSION="2.2.0"/>`, "missing required element"},
		{"empty-input", ``, "invalid XML"},
		{"comparam-spec-root", `<ODX MODEL-VERSION="2.2.0"><COMPARAM-SPEC ID="X"><SHORT-NAME>X</SHORT-NAME></COMPARAM-SPEC></ODX>`, "not supported as the root category"},

		// ---- SHORT-NAME / LONG-NAME / ID ----
		{"short-name-hyphen", rep(`<SHORT-NAME>TestDL</SHORT-NAME>`, `<SHORT-NAME>Test-DL</SHORT-NAME>`), "does not match pattern"},
		{"long-name-too-long", rep(`<SHORT-NAME>TestDL</SHORT-NAME>`, `<SHORT-NAME>TestDL</SHORT-NAME><LONG-NAME>`+strings.Repeat("a", 256)+`</LONG-NAME>`), "at most 255 characters"},
		{"duplicate-id", rep(`ID="REQ_1"`, `ID="SVC_1"`), "duplicate ID"},

		// ---- element ordering ----
		{"dtc-dops-after-dops", variant(`<DIAG-DATA-DICTIONARY-SPEC><DATA-OBJECT-PROPS/><DTC-DOPS/></DIAG-DATA-DICTIONARY-SPEC>`), "DTC-DOPS"},
		{"dictionary-after-comms", variant(`<DIAG-COMMS><DIAG-SERVICE ID="S"><SHORT-NAME>S</SHORT-NAME></DIAG-SERVICE></DIAG-COMMS><DIAG-DATA-DICTIONARY-SPEC/>`), "unexpected element <DIAG-DATA-DICTIONARY-SPEC>"},
		{"dtc-dop-missing-physical-type", variant(strings.Replace(dtcDopValid, `<PHYSICAL-TYPE BASE-DATA-TYPE="A_UINT32"/>`, "", 1)), "expected <PHYSICAL-TYPE>"},
		{"dtc-dop-missing-compu-method", variant(strings.Replace(dtcDopValid, `<COMPU-METHOD><CATEGORY>IDENTICAL</CATEGORY></COMPU-METHOD>`, "", 1)), "expected <COMPU-METHOD>"},
		{"dtc-dop-compu-after-dtcs", variant(`<DIAG-DATA-DICTIONARY-SPEC><DTC-DOPS><DTC-DOP ID="X"><SHORT-NAME>X</SHORT-NAME><DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"><BIT-LENGTH>8</BIT-LENGTH></DIAG-CODED-TYPE><PHYSICAL-TYPE BASE-DATA-TYPE="A_UINT32"/><DTCS><DTC ID="T"><SHORT-NAME>T</SHORT-NAME><TROUBLE-CODE>1</TROUBLE-CODE><TEXT>x</TEXT></DTC></DTCS><COMPU-METHOD><CATEGORY>IDENTICAL</CATEGORY></COMPU-METHOD></DTC-DOP></DTC-DOPS></DIAG-DATA-DICTIONARY-SPEC>`), "<COMPU-METHOD>"},
		{"201-dtc-dop-missing-compu-method", variant201(strings.Replace(dtcDopValid, `<COMPU-METHOD><CATEGORY>IDENTICAL</CATEGORY></COMPU-METHOD>`, "", 1)), "expected <COMPU-METHOD>"},
		{"request-fields-out-of-order", variant(`<REQUESTS><REQUEST ID="R"><PARAMS/><SHORT-NAME>R</SHORT-NAME></REQUEST></REQUESTS>`), "out of order"},

		// ---- polymorphism via xsi:type ----
		{"param-without-xsitype", repAll(`<PARAM xsi:type="CODED-CONST" SEMANTIC="SERVICE-ID">`, `<PARAM SEMANTIC="SERVICE-ID">`), "abstract type PARAM"},
		{"diag-coded-type-without-xsitype", repAll(` xsi:type="STANDARD-LENGTH-TYPE"`, ``), "abstract type DIAG-CODED-TYPE"},
		{"coded-const-as-element", strings.ReplaceAll(strings.ReplaceAll(base, `<PARAM xsi:type="CODED-CONST" SEMANTIC="SERVICE-ID">`, `<CODED-CONST SEMANTIC="SERVICE-ID">`), `</PARAM>`, `</CODED-CONST>`), "expected <PARAM>"},
		{"standard-length-missing-bit-length", rep(`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE">
                <BIT-LENGTH>24</BIT-LENGTH>
              </DIAG-CODED-TYPE>`, `<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"/>`), "missing required element <BIT-LENGTH>"},
		{"unknown-xsitype", rep(`xsi:type="STANDARD-LENGTH-TYPE">`, `xsi:type="BOGUS-TYPE">`), "unknown xsi:type"},
		{"unrelated-xsitype", rep(`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE">
                <BIT-LENGTH>24</BIT-LENGTH>
              </DIAG-CODED-TYPE>`, `<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="PHYSICAL-TYPE"/>`), "is not a subtype of"},

		// ---- enumerations / facets ----
		{"bad-compu-category", rep(`<CATEGORY>IDENTICAL</CATEGORY>`, `<CATEGORY>BOGUS</CATEGORY>`), `invalid value "BOGUS" for <CATEGORY>`},
		{"bad-diag-coded-data-type", repAll(`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE">`, `<DIAG-CODED-TYPE BASE-DATA-TYPE="NOPE" xsi:type="STANDARD-LENGTH-TYPE">`), "invalid value"},
		{"bad-physical-data-type", rep(`<PHYSICAL-TYPE BASE-DATA-TYPE="A_UINT32"/>`, `<PHYSICAL-TYPE BASE-DATA-TYPE="A_ASCIISTRING"/>`), "invalid value"},
		{"bad-addressing", rep(`<DIAG-SERVICE ID="SVC_1">`, `<DIAG-SERVICE ID="SVC_1" ADDRESSING="BOGUS">`), "ADDRESSING"},
		{"bad-transmission-mode", rep(`<DIAG-SERVICE ID="SVC_1">`, `<DIAG-SERVICE ID="SVC_1" TRANSMISSION-MODE="BOGUS">`), "TRANSMISSION-MODE"},
		{"bad-interval-type", rep(`<COMPU-RATIONAL-COEFFS>`, `<LOWER-LIMIT INTERVAL-TYPE="HALF">0</LOWER-LIMIT><COMPU-RATIONAL-COEFFS>`), "INTERVAL-TYPE"},
		{"bit-length-zero", rep(`<BIT-LENGTH>24</BIT-LENGTH>`, `<BIT-LENGTH>0</BIT-LENGTH>`), "must be > 0"},
		{"bit-mask-not-hex", rep(`<BIT-LENGTH>24</BIT-LENGTH>`, `<BIT-LENGTH>24</BIT-LENGTH><BIT-MASK>ZZ</BIT-MASK>`), "hexBinary"},
		{"trouble-code-not-int", rep(`<TROUBLE-CODE>65696</TROUBLE-CODE>`, `<TROUBLE-CODE>abc</TROUBLE-CODE>`), "expected xsd:unsignedInt"},
		{"level-out-of-byte-range", rep(`<TEXT>Mass Air Flow Sensor Fault</TEXT>`, `<TEXT>Mass Air Flow Sensor Fault</TEXT><LEVEL>256</LEVEL>`), "unsignedByte"},
		{"bit-position-not-lt-8", rep(`<BYTE-POSITION>0</BYTE-POSITION>`, `<BYTE-POSITION>0</BYTE-POSITION><BIT-POSITION>8</BIT-POSITION>`), "must be < 8"},

		// ---- required children / unknown nodes ----
		{"request-missing-params", variant(`<REQUESTS><REQUEST ID="R"><SHORT-NAME>R</SHORT-NAME></REQUEST></REQUESTS>`), "missing required element <PARAMS>"},
		{"value-missing-dop-ref", variant(`<REQUESTS><REQUEST ID="R"><SHORT-NAME>R</SHORT-NAME><PARAMS><PARAM xsi:type="VALUE"><SHORT-NAME>V</SHORT-NAME></PARAM></PARAMS></REQUEST></REQUESTS>`), "missing required element <DOP-REF or DOP-SNREF>"},
		{"matching-request-param-missing-byte-length", variant(`<REQUESTS><REQUEST ID="R"><SHORT-NAME>R</SHORT-NAME><PARAMS><PARAM xsi:type="MATCHING-REQUEST-PARAM"><SHORT-NAME>M</SHORT-NAME><REQUEST-BYTE-POS>0</REQUEST-BYTE-POS></PARAM></PARAMS></REQUEST></REQUESTS>`), "missing required element <BYTE-LENGTH>"},
		{"201-desc-bare-text", variant201(`<DESC>plain</DESC>`), "character data is not allowed in <DESC>"},
		{"compu-numerator-text", rep(`<COMPU-NUMERATOR>
                          <V>1</V>
                          <V>0</V>
                        </COMPU-NUMERATOR>`, `<COMPU-NUMERATOR>1 0</COMPU-NUMERATOR>`), "character data is not allowed"},
		{"compu-const-two-children", rep(`<COMPU-RATIONAL-COEFFS>
                        <COMPU-NUMERATOR>
                          <V>1</V>
                          <V>0</V>
                        </COMPU-NUMERATOR>
                      </COMPU-RATIONAL-COEFFS>`, `<COMPU-CONST><V>0</V><VT>x</VT></COMPU-CONST>`), "unexpected element <VT>"},
		{"unknown-element", rep(`</SHORT-NAME>
    <ECU-VARIANTS>`, `</SHORT-NAME>
    <BOGUS/>
    <ECU-VARIANTS>`), "unexpected element <BOGUS>"},
		{"unknown-attribute", rep(`<DIAG-SERVICE ID="SVC_1">`, `<DIAG-SERVICE ID="SVC_1" BOGUS="x">`), "unexpected attribute BOGUS"},
		{"missing-service-id", rep(`<DIAG-SERVICE ID="SVC_1">`, `<DIAG-SERVICE>`), "missing required attribute ID"},

		// ---- references (ODXLINK / SNREF / SNPATHREF) ----
		{"odxlink-missing-id-ref", rep(`<REQUEST-REF ID-REF="REQ_1"/>`, `<REQUEST-REF/>`), "missing required attribute ID-REF"},
		{"odxlink-docref-pattern", rep(`<REQUEST-REF ID-REF="REQ_1"/>`, `<REQUEST-REF ID-REF="REQ_1" DOCREF="bad-name"/>`), "attribute DOCREF"},
		{"snref-missing-short-name", variant(valueParam(`<DOP-SNREF/>`)), "missing required attribute SHORT-NAME on <DOP-SNREF>"},
		{"snref-pattern", variant(valueParam(`<DOP-SNREF SHORT-NAME="bad-name"/>`)), "SHORT-NAME on <DOP-SNREF>"},
		{"snpathref-missing-path", variant(envDataDesc(`<PARAM-SNPATHREF/>`)), "missing required attribute SHORT-NAME-PATH"},
		{"snpathref-pattern", variant(envDataDesc(`<PARAM-SNPATHREF SHORT-NAME-PATH="A..B"/>`)), "SHORT-NAME-PATH on <PARAM-SNPATHREF>"},

		// ---- version differences (2.0.1 rejects 2.2.0-only constructs) ----
		{"201-comparam-subset-root", `<ODX MODEL-VERSION="2.0.1"><COMPARAM-SUBSET/></ODX>`, "unexpected element <COMPARAM-SUBSET>"},
		{"201-linked-dtc-dops", variant201(strings.Replace(dtcDopValid, `</DTC-DOP>`, `<LINKED-DTC-DOPS/></DTC-DOP>`, 1)), "unexpected element <LINKED-DTC-DOPS>"},
		{"201-standard-length-missing-bit-length", variant201(strings.Replace(dtcDopValid,
			`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"><BIT-LENGTH>8</BIT-LENGTH></DIAG-CODED-TYPE>`,
			`<DIAG-CODED-TYPE BASE-DATA-TYPE="A_UINT32" xsi:type="STANDARD-LENGTH-TYPE"/>`, 1)), "missing required element <BIT-LENGTH>"},

		// ---- inherited attributes / wrapper children ----
		{"dtc-dop-missing-inherited-id", variant(strings.Replace(dtcDopValid, `<DTC-DOP ID="X">`, `<DTC-DOP>`, 1)), "missing required attribute ID on <DTC-DOP>"},
		{"wrapper-missing-child", variant(`<DIAG-DATA-DICTIONARY-SPEC><DTC-DOPS/></DIAG-DATA-DICTIONARY-SPEC>`), "missing required element <DTC-DOP>"},

		// ---- rich text ----
		{"desc-bare-text-220", variant(`<DESC>plain</DESC>`), "character data is not allowed in <DESC>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateODX([]byte(tc.doc))
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tc.want)
			}
			got := strings.Join(errs, "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected error containing %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func makePDX(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestPDX(t *testing.T) {
	valid := []byte(mustRead(t, "testdata/minimal.odx"))
	invalid := []byte(`<ODX MODEL-VERSION="2.2.0"><COMPARAM-SPEC/></ODX>`)

	t.Run("all-valid", func(t *testing.T) {
		pdx := makePDX(t, map[string][]byte{"db.odx": valid, "extra.xml": valid})
		if errs := verifyPDX(pdx); len(errs) != 0 {
			t.Fatalf("expected valid PDX, got:\n%s", strings.Join(errs, "\n"))
		}
	})
	t.Run("invalid-entry", func(t *testing.T) {
		pdx := makePDX(t, map[string][]byte{"bad.odx": invalid})
		errs := verifyPDX(pdx)
		if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "bad.odx:") {
			t.Fatalf("expected error for bad.odx, got: %v", errs)
		}
	})
	t.Run("not-a-zip", func(t *testing.T) {
		errs := verifyPDX([]byte("not a zip"))
		if len(errs) == 0 || !strings.Contains(errs[0], "not a zip archive") {
			t.Fatalf("expected zip error, got: %v", errs)
		}
	})
}

package kepub

import (
	"strings"
	"testing"
)

func TestAddCalibreSeriesMetadata_FillsMissingFields(t *testing.T) {
	opf, err := addCalibreSeriesMetadata([]byte(`<package><metadata/></package>`), Metadata{Series: "The <Series>", SeriesIndex: 2.5})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="calibre:series" content="The &lt;Series&gt;"`, `name="calibre:series_index" content="2.5"`} {
		if !strings.Contains(string(opf), want) {
			t.Errorf("OPF missing %q: %s", want, opf)
		}
	}
}

func TestAddCalibreSeriesMetadata_PreservesExistingFields(t *testing.T) {
	source := []byte(`<package><metadata><meta name="calibre:series" content="Original"/><meta name="calibre:series_index" content="1"/></metadata></package>`)
	opf, err := addCalibreSeriesMetadata(source, Metadata{Series: "Replacement", SeriesIndex: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(opf); got != string(source) {
		t.Errorf("OPF changed despite existing Calibre metadata: %s", got)
	}
}

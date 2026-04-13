package document

import (
	"context"
	"testing"
)

type stubVirtualSource struct {
	ReadOnlySource
}

func (s *stubVirtualSource) GetList(_ context.Context, _ ListOptions) ([]map[string]any, int, error) {
	return nil, 0, nil
}
func (s *stubVirtualSource) GetOne(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func TestVirtualSourceRegistry_RegisterAndGet(t *testing.T) {
	reg := NewVirtualSourceRegistry()
	src := &stubVirtualSource{}
	reg.Register("ExternalInvoice", src)

	got, ok := reg.Get("ExternalInvoice")
	if !ok {
		t.Fatal("expected Get to return true for registered source")
	}
	if got != src {
		t.Error("expected Get to return the same source instance")
	}

	_, ok = reg.Get("NonExistent")
	if ok {
		t.Error("expected Get to return false for unregistered source")
	}
}

func TestVirtualSourceRegistry_List(t *testing.T) {
	reg := NewVirtualSourceRegistry()
	reg.Register("Beta", &stubVirtualSource{})
	reg.Register("Alpha", &stubVirtualSource{})

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	if names[0] != "Alpha" || names[1] != "Beta" {
		t.Errorf("expected sorted [Alpha Beta], got %v", names)
	}
}

func TestVirtualSourceRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewVirtualSourceRegistry()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			reg.Register("Type", &stubVirtualSource{})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		reg.Get("Type")
		reg.List()
	}
	<-done
}

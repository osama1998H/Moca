package document

import "testing"

var lifecycleBenchmarkErrSink error

func BenchmarkDispatchEvent(b *testing.B) {
	ctrl := BaseController{}
	ctx := &DocContext{}
	doc := &DynamicDoc{}

	events := []DocEvent{
		EventBeforeInsert,
		EventAfterInsert,
		EventBeforeValidate,
		EventValidate,
		EventBeforeSave,
		EventAfterSave,
		EventOnUpdate,
		EventBeforeSubmit,
		EventOnSubmit,
		EventBeforeCancel,
		EventOnCancel,
		EventOnTrash,
		EventAfterDelete,
		EventOnChange,
	}

	for _, event := range events {
		b.Run(string(event), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				err := dispatchEvent(ctrl, event, ctx, doc)
				if err != nil {
					b.Fatalf("dispatchEvent(%q): %v", event, err)
				}
				lifecycleBenchmarkErrSink = err
			}
		})
	}
}

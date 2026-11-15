package requests

import (
	"context"
	"testing"
)

// A show page finds the person's open 4K ask apart from the plain one, so a
// show in the Library offers its own Ask in 4K and says when it was asked.
func TestOpenShow4KAskFindsOnlyThe4KAsk(t *testing.T) {
	r := showRig(t)
	ctx := context.Background()
	ids := map[string]string{"tmdb": "100"}
	if b, err := OpenShow4KAsk(ctx, r.st.Read(), ids, r.bo.UserID); err != nil || b != nil {
		t.Fatalf("no 4K ask yet: %+v %v", b, err)
	}
	if _, err := r.svc.AskShow(ctx, r.ana, "tmdb:100", []int{1}); err != nil {
		t.Fatal(err)
	}
	if b, err := OpenShow4KAsk(ctx, r.st.Read(), ids, r.ana.UserID); err != nil || b != nil {
		t.Fatalf("a plain ask read as 4K: %+v %v", b, err)
	}
	uhd, err := r.svc.AskShowFourK(ctx, r.bo, "tmdb:100", []int{1}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenShow4KAsk(ctx, r.st.Read(), ids, r.bo.UserID)
	if err != nil || b == nil || b.ID != uhd.Ask.ID || !b.Mine || len(b.Seasons) != 1 {
		t.Fatalf("4K ask %+v %v", b, err)
	}
	if b, _ := OpenShow4KAsk(ctx, r.st.Read(), ids, r.ana.UserID); b == nil || b.Mine {
		t.Fatalf("another person's view %+v", b)
	}
}

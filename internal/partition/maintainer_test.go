package partition

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

func TestPartitionName(t *testing.T) {
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if got, want := partitionName(ts), "traces_202605"; got != want {
		t.Errorf("partitionName = %q, want %q", got, want)
	}
}

func TestParsePartitionMonth(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"traces_202605", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"traces_202612", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), true},
		{"projects", time.Time{}, false},
		{"traces_foo", time.Time{}, false},
		{"traces_2026", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := parsePartitionMonth(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAddMonthsWrapsYear(t *testing.T) {
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got := addMonths(base, 3) // Feb 2027
	want := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("addMonths +3 = %v, want %v", got, want)
	}
	got2 := addMonths(base, -12) // Nov 2025
	want2 := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	if !got2.Equal(want2) {
		t.Errorf("addMonths -12 = %v, want %v", got2, want2)
	}
}

// fakeProjectRepo implements just enough of repo.ProjectRepo for purge tests.
// Only List is exercised — every other method panics so an accidental call
// surfaces as a test failure instead of a silent zero value.
type fakeProjectRepo struct {
	projects []*repo.Project
	err      error
	listN    int
}

func (f *fakeProjectRepo) List(context.Context) ([]*repo.Project, error) {
	f.listN++
	return f.projects, f.err
}

func (f *fakeProjectRepo) Create(context.Context, *repo.Project) error { panic("not used") }
func (f *fakeProjectRepo) GetByID(context.Context, string) (*repo.Project, error) {
	panic("not used")
}
func (f *fakeProjectRepo) GetByProjectKey(context.Context, string) (*repo.Project, error) {
	panic("not used")
}
func (f *fakeProjectRepo) Update(context.Context, *repo.Project) error            { panic("not used") }
func (f *fakeProjectRepo) UpdateProjectKey(context.Context, string, string) error { panic("not used") }
func (f *fakeProjectRepo) Delete(context.Context, string) error                   { panic("not used") }

// fakeBodyStore records every DeletePrefix call. Optional failOn map returns
// an error for matching prefixes; everything else "succeeds".
type fakeBodyStore struct {
	mu     sync.Mutex
	calls  []string
	failOn map[string]error
}

func (f *fakeBodyStore) DeletePrefix(_ context.Context, prefix string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, prefix)
	if err, ok := f.failOn[prefix]; ok {
		return err
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newMaintainerForPurgeTest(projects repo.ProjectRepo, bs BodyPurger) *Maintainer {
	// pool is nil — purgeBodiesForMonths never touches it.
	return &Maintainer{
		pool:      nil,
		logger:    discardLogger(),
		cfg:       Config{},
		projects:  projects,
		bodyStore: bs,
		done:      make(chan struct{}),
	}
}

func TestPurgeBodiesForMonths_NoopWhenDependenciesMissing(t *testing.T) {
	months := []time.Time{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	bs := &fakeBodyStore{}
	projects := &fakeProjectRepo{projects: []*repo.Project{{ID: "p1"}}}

	cases := []struct {
		name string
		m    *Maintainer
	}{
		{"nil projects", newMaintainerForPurgeTest(nil, bs)},
		{"nil bodystore", newMaintainerForPurgeTest(projects, nil)},
		{"both nil", newMaintainerForPurgeTest(nil, nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bs.calls = nil
			projects.listN = 0
			if err := c.m.purgeBodiesForMonths(context.Background(), months); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(bs.calls) != 0 {
				t.Errorf("expected no DeletePrefix calls, got %v", bs.calls)
			}
			if projects.listN != 0 {
				t.Errorf("expected no List calls, got %d", projects.listN)
			}
		})
	}
}

func TestPurgeBodiesForMonths_NoopWhenMonthsEmpty(t *testing.T) {
	bs := &fakeBodyStore{}
	projects := &fakeProjectRepo{projects: []*repo.Project{{ID: "p1"}}}
	m := newMaintainerForPurgeTest(projects, bs)

	if err := m.purgeBodiesForMonths(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projects.listN != 0 {
		t.Errorf("expected List to be skipped on empty months, got %d calls", projects.listN)
	}
	if len(bs.calls) != 0 {
		t.Errorf("expected no DeletePrefix calls, got %v", bs.calls)
	}
}

func TestPurgeBodiesForMonths_HappyPath(t *testing.T) {
	bs := &fakeBodyStore{}
	projects := &fakeProjectRepo{projects: []*repo.Project{
		{ID: "proj_a"}, {ID: "proj_b"},
	}}
	m := newMaintainerForPurgeTest(projects, bs)

	months := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := m.purgeBodiesForMonths(context.Background(), months); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := append([]string(nil), bs.calls...)
	sort.Strings(got)
	want := []string{
		"proj_a/202601/",
		"proj_a/202602/",
		"proj_b/202601/",
		"proj_b/202602/",
	}
	if len(got) != len(want) {
		t.Fatalf("DeletePrefix calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPurgeBodiesForMonths_ContinuesOnPartialFailure(t *testing.T) {
	boom := errors.New("boom")
	bs := &fakeBodyStore{
		failOn: map[string]error{
			"proj_a/202601/": boom, // first prefix fails — must not abort the loop
		},
	}
	projects := &fakeProjectRepo{projects: []*repo.Project{
		{ID: "proj_a"}, {ID: "proj_b"},
	}}
	m := newMaintainerForPurgeTest(projects, bs)

	months := []time.Time{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	err := m.purgeBodiesForMonths(context.Background(), months)
	if !errors.Is(err, boom) {
		t.Fatalf("expected first error %v to bubble up, got %v", boom, err)
	}
	// Both prefixes should have been attempted; failure on the first must not
	// stop the second project from being processed.
	if len(bs.calls) != 2 {
		t.Fatalf("expected 2 attempted DeletePrefix calls, got %v", bs.calls)
	}
}

func TestPurgeBodiesForMonths_ListProjectsFails(t *testing.T) {
	boom := errors.New("db down")
	bs := &fakeBodyStore{}
	projects := &fakeProjectRepo{err: boom}
	m := newMaintainerForPurgeTest(projects, bs)

	months := []time.Time{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	err := m.purgeBodiesForMonths(context.Background(), months)
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped err %v, got %v", boom, err)
	}
	if len(bs.calls) != 0 {
		t.Errorf("no DeletePrefix should run when project list fails, got %v", bs.calls)
	}
}

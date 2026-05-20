package setup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/replication"
)

// fakeExecutor는 Exec/QueryBool/QueryStrings 호출을 기록하는 in-memory test
// fixture입니다.
type fakeExecutor struct {
	mu sync.Mutex

	// boolReturns: 다음 QueryBool 호출이 반환할 값 (FIFO queue). 비어있으면 false.
	boolReturns []bool
	boolErrs    []error

	// stringsReturns: 다음 QueryStrings 호출이 반환할 slice (FIFO). 비어있으면 nil.
	stringsReturns [][]string
	stringsErrs    []error

	// execErr: 모든 Exec 호출이 반환할 에러 (nil이면 성공).
	execErr error

	// 기록.
	execCalls         []execCall
	queryBoolCalls    []queryCall
	queryStringsCalls []queryCall
}

type execCall struct {
	sql  string
	args []any
}

type queryCall struct {
	sql  string
	args []any
}

func (f *fakeExecutor) Exec(_ context.Context, sql string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execCalls = append(f.execCalls, execCall{sql: sql, args: args})
	return f.execErr
}

func (f *fakeExecutor) QueryBool(_ context.Context, sql string, args ...any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryBoolCalls = append(f.queryBoolCalls, queryCall{sql: sql, args: args})
	if len(f.boolErrs) > 0 {
		err := f.boolErrs[0]
		f.boolErrs = f.boolErrs[1:]
		if err != nil {
			return false, err
		}
	}
	if len(f.boolReturns) > 0 {
		v := f.boolReturns[0]
		f.boolReturns = f.boolReturns[1:]
		return v, nil
	}
	return false, nil
}

func (f *fakeExecutor) QueryStrings(_ context.Context, sql string, args ...any) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryStringsCalls = append(f.queryStringsCalls, queryCall{sql: sql, args: args})
	if len(f.stringsErrs) > 0 {
		err := f.stringsErrs[0]
		f.stringsErrs = f.stringsErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.stringsReturns) > 0 {
		v := f.stringsReturns[0]
		f.stringsReturns = f.stringsReturns[1:]
		return v, nil
	}
	return nil, nil
}

func (f *fakeExecutor) lastExecSQL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.execCalls) == 0 {
		return ""
	}
	return f.execCalls[len(f.execCalls)-1].sql
}

// --- Setup dispatch ---------------------------------------------------------

func TestSetup_PrimaryWithoutPublicationSpec_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	err := Setup(context.Background(), fake, replication.RolePrimary, nil, nil)
	if !errors.Is(err, ErrPublicationSpecMissing) {
		t.Fatalf("want ErrPublicationSpecMissing, got %v", err)
	}
}

func TestSetup_StandbyWithoutSubscriptionSpec_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	err := Setup(context.Background(), fake, replication.RoleStandby, nil, nil)
	if !errors.Is(err, ErrSubscriptionSpecMissing) {
		t.Fatalf("want ErrSubscriptionSpecMissing, got %v", err)
	}
}

func TestSetup_UnknownRole_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	err := Setup(context.Background(), fake, replication.Role("unknown"), nil, nil)
	if !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("want ErrUnknownRole, got %v", err)
	}
}

// --- ensurePublication ------------------------------------------------------

func TestEnsurePublication_AllTables_NewlyCreated(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := PublicationSpec{Name: "rosshield_main", AllTables: true}

	if err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if got := len(fake.queryBoolCalls); got != 1 {
		t.Fatalf("want 1 existence check, got %d", got)
	}
	if !strings.Contains(fake.queryBoolCalls[0].sql, "pg_publication") {
		t.Fatalf("existence query missing pg_publication: %q", fake.queryBoolCalls[0].sql)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 CREATE exec, got %d", got)
	}
	sqlStmt := fake.lastExecSQL()
	if !strings.Contains(sqlStmt, "CREATE PUBLICATION") {
		t.Fatalf("missing CREATE PUBLICATION: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, `"rosshield_main"`) {
		t.Fatalf("missing quoted name: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, "FOR ALL TABLES") {
		t.Fatalf("missing FOR ALL TABLES: %q", sqlStmt)
	}
}

func TestEnsurePublication_AlreadyExists_Skip(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{true}}
	spec := PublicationSpec{Name: "rosshield_main", AllTables: true}

	if err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("want 0 CREATE exec (skip), got %d", got)
	}
}

func TestEnsurePublication_ExplicitTables(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"tenants", "audit_entries"},
	}

	if err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	sqlStmt := fake.lastExecSQL()
	if !strings.Contains(sqlStmt, "FOR TABLE") {
		t.Fatalf("missing FOR TABLE: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, `"tenants"`) {
		t.Fatalf("missing quoted tenants: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, `"audit_entries"`) {
		t.Fatalf("missing quoted audit_entries: %q", sqlStmt)
	}
}

func TestEnsurePublication_EmptyTables_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    nil,
	}

	err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil)
	if !errors.Is(err, ErrEmptyTables) {
		t.Fatalf("want ErrEmptyTables, got %v", err)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("no CREATE should be issued, got %d", got)
	}
}

func TestEnsurePublication_InvalidName_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	spec := PublicationSpec{Name: "drop;table", AllTables: true}

	err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("want invalid identifier error, got %v", err)
	}
	if got := len(fake.queryBoolCalls); got != 0 {
		t.Fatalf("validation should short-circuit before existence check, got %d", got)
	}
}

func TestEnsurePublication_ExistenceQueryError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("conn dropped")
	fake := &fakeExecutor{boolErrs: []error{wantErr}}
	spec := PublicationSpec{Name: "rosshield_main", AllTables: true}

	err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("want wrap of %v, got %v", wantErr, err)
	}
}

func TestEnsurePublication_CreateError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("permission denied")
	fake := &fakeExecutor{boolReturns: []bool{false}, execErr: wantErr}
	spec := PublicationSpec{Name: "rosshield_main", AllTables: true}

	err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("want wrap of %v, got %v", wantErr, err)
	}
}

// --- syncPublicationTables (publication tables 변경 자동 동기화) ---------

func TestSyncPublicationTables_AllTables_NoOp(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	spec := PublicationSpec{Name: "rosshield_main", AllTables: true}

	if err := syncPublicationTables(context.Background(), fake, spec); err != nil {
		t.Fatalf("syncPublicationTables: %v", err)
	}
	if got := len(fake.queryStringsCalls); got != 0 {
		t.Fatalf("AllTables=true 경로는 QueryStrings 호출하지 않습니다, got %d", got)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("AllTables=true 경로는 ALTER 호출하지 않습니다, got %d", got)
	}
}

func TestSyncPublicationTables_AddTable(t *testing.T) {
	t.Parallel()
	// 기존 publication에는 tenants만 있고 spec은 tenants+audit_entries 요구 →
	// audit_entries ADD TABLE 1건.
	fake := &fakeExecutor{stringsReturns: [][]string{{"tenants"}}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"tenants", "audit_entries"},
	}

	if err := syncPublicationTables(context.Background(), fake, spec); err != nil {
		t.Fatalf("syncPublicationTables: %v", err)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 ALTER (ADD), got %d", got)
	}
	got := fake.lastExecSQL()
	if !strings.Contains(got, "ALTER PUBLICATION") {
		t.Fatalf("missing ALTER PUBLICATION: %q", got)
	}
	if !strings.Contains(got, "ADD TABLE") {
		t.Fatalf("missing ADD TABLE: %q", got)
	}
	if !strings.Contains(got, `"audit_entries"`) {
		t.Fatalf("missing audit_entries: %q", got)
	}
	if strings.Contains(got, `"tenants"`) {
		t.Fatalf("기존 tenants가 ADD 절에 포함되면 안 됩니다: %q", got)
	}
}

func TestSyncPublicationTables_DropTable(t *testing.T) {
	t.Parallel()
	// 기존 publication: tenants+audit_entries, spec: tenants → audit_entries
	// DROP TABLE.
	fake := &fakeExecutor{stringsReturns: [][]string{{"tenants", "audit_entries"}}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"tenants"},
	}

	if err := syncPublicationTables(context.Background(), fake, spec); err != nil {
		t.Fatalf("syncPublicationTables: %v", err)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 ALTER (DROP), got %d", got)
	}
	got := fake.lastExecSQL()
	if !strings.Contains(got, "DROP TABLE") {
		t.Fatalf("missing DROP TABLE: %q", got)
	}
	if !strings.Contains(got, `"audit_entries"`) {
		t.Fatalf("missing audit_entries in DROP: %q", got)
	}
}

func TestSyncPublicationTables_BothAddAndDrop(t *testing.T) {
	t.Parallel()
	// 기존: tenants, scans / spec: tenants, audit_entries → ADD audit_entries +
	// DROP scans. 두 ALTER 호출 발생.
	fake := &fakeExecutor{stringsReturns: [][]string{{"tenants", "scans"}}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"tenants", "audit_entries"},
	}

	if err := syncPublicationTables(context.Background(), fake, spec); err != nil {
		t.Fatalf("syncPublicationTables: %v", err)
	}
	if got := len(fake.execCalls); got != 2 {
		t.Fatalf("want 2 ALTER (ADD+DROP), got %d", got)
	}
	addFound, dropFound := false, false
	for _, c := range fake.execCalls {
		if strings.Contains(c.sql, "ADD TABLE") && strings.Contains(c.sql, `"audit_entries"`) {
			addFound = true
		}
		if strings.Contains(c.sql, "DROP TABLE") && strings.Contains(c.sql, `"scans"`) {
			dropFound = true
		}
	}
	if !addFound {
		t.Fatalf("ADD audit_entries 누락: %+v", fake.execCalls)
	}
	if !dropFound {
		t.Fatalf("DROP scans 누락: %+v", fake.execCalls)
	}
}

func TestSyncPublicationTables_NoChange(t *testing.T) {
	t.Parallel()
	// 기존과 spec이 동일 → ALTER 0회.
	fake := &fakeExecutor{stringsReturns: [][]string{{"tenants", "audit_entries"}}}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"audit_entries", "tenants"}, // 순서 무관해야 함
	}

	if err := syncPublicationTables(context.Background(), fake, spec); err != nil {
		t.Fatalf("syncPublicationTables: %v", err)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("동일 set → 0 ALTER expected, got %d", got)
	}
}

func TestEnsurePublication_ExistsAndExplicitTables_TriggersSync(t *testing.T) {
	t.Parallel()
	// publication exists=true + AllTables=false → sync 경로로 진입해야 함.
	fake := &fakeExecutor{
		boolReturns:    []bool{true},
		stringsReturns: [][]string{{"tenants"}},
	}
	spec := PublicationSpec{
		Name:      "rosshield_main",
		AllTables: false,
		Tables:    []string{"tenants", "audit_entries"},
	}

	if err := Setup(context.Background(), fake, replication.RolePrimary, &spec, nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if got := len(fake.queryStringsCalls); got != 1 {
		t.Fatalf("want 1 QueryStrings (sync trigger), got %d", got)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 ALTER (ADD audit_entries), got %d", got)
	}
	if !strings.Contains(fake.lastExecSQL(), "ADD TABLE") {
		t.Fatalf("missing ADD TABLE: %q", fake.lastExecSQL())
	}
}

// --- CleanupInactiveSlots (replication slot 자동 cleanup) -----------------

func TestCleanupInactiveSlots_DropsOne(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{stringsReturns: [][]string{{"rosshield_main_sub"}}}
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 24 * time.Hour,
		SlotPrefix:     "rosshield_",
	}

	removed, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if len(removed) != 1 || removed[0] != "rosshield_main_sub" {
		t.Fatalf("removed list want [rosshield_main_sub], got %v", removed)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 pg_drop_replication_slot exec, got %d", got)
	}
	call := fake.execCalls[0]
	if !strings.Contains(call.sql, "pg_drop_replication_slot") {
		t.Fatalf("missing pg_drop_replication_slot: %q", call.sql)
	}
	// bind parameter로 slot 이름 전달 (SQL injection 방지)
	if len(call.args) != 1 {
		t.Fatalf("want 1 bind arg (slot name), got %d (%v)", len(call.args), call.args)
	}
	if call.args[0] != "rosshield_main_sub" {
		t.Fatalf("missing slot name in args: %v", call.args)
	}
}

func TestCleanupInactiveSlots_DryRun_NoDrop(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{stringsReturns: [][]string{{"rosshield_main_sub"}}}
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 24 * time.Hour,
		SlotPrefix:     "rosshield_",
		DryRun:         true,
	}

	removed, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if len(removed) != 1 || removed[0] != "rosshield_main_sub" {
		t.Fatalf("DryRun도 candidate list 반환 — want [rosshield_main_sub], got %v", removed)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("DryRun=true → 0 Exec expected, got %d", got)
	}
}

func TestCleanupInactiveSlots_PrefixMismatch_Skip(t *testing.T) {
	t.Parallel()
	// PG가 prefix 필터링을 했지만, 방어적으로 client측에서도 prefix 검증해야 함.
	// 만약 PG가 반환한 slot이 prefix를 만족하지 않으면 skip.
	fake := &fakeExecutor{stringsReturns: [][]string{{"other_app_slot", "rosshield_legit"}}}
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 24 * time.Hour,
		SlotPrefix:     "rosshield_",
	}

	removed, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if len(removed) != 1 || removed[0] != "rosshield_legit" {
		t.Fatalf("prefix-mismatch slot은 skip — want [rosshield_legit], got %v", removed)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 drop exec (legit만), got %d", got)
	}
	call := fake.execCalls[0]
	if len(call.args) != 1 || call.args[0] != "rosshield_legit" {
		t.Fatalf("legit slot 누락 in bind args: %v", call.args)
	}
}

func TestCleanupInactiveSlots_EmptyList_Graceful(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{stringsReturns: [][]string{nil}}
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 24 * time.Hour,
		SlotPrefix:     "rosshield_",
	}

	removed, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("empty list want 0, got %v", removed)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("want 0 exec, got %d", got)
	}
}

func TestCleanupInactiveSlots_EmptyPrefix_Error(t *testing.T) {
	t.Parallel()
	// 안전 가드 — prefix가 비면 모든 slot이 drop 후보가 되므로 explicit error.
	fake := &fakeExecutor{}
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 24 * time.Hour,
		SlotPrefix:     "",
	}

	_, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if !errors.Is(err, ErrEmptySlotPrefix) {
		t.Fatalf("want ErrEmptySlotPrefix, got %v", err)
	}
}

func TestCleanupInactiveSlots_DefaultMinAge(t *testing.T) {
	t.Parallel()
	// MinInactiveAge=0 → default 24h 적용. SQL에 86400 (24*3600) seconds 노출 확인.
	fake := &fakeExecutor{stringsReturns: [][]string{nil}}
	opts := CleanupInactiveSlotsOptions{
		SlotPrefix: "rosshield_",
	}

	if _, err := CleanupInactiveSlots(context.Background(), fake, opts); err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if got := len(fake.queryStringsCalls); got != 1 {
		t.Fatalf("want 1 QueryStrings, got %d", got)
	}
	args := fake.queryStringsCalls[0].args
	if len(args) < 2 {
		t.Fatalf("want >=2 args (prefix, age), got %d (%v)", len(args), args)
	}
}

func TestCleanupInactiveSlots_WithLogger(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{stringsReturns: [][]string{{"rosshield_main_sub"}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	opts := CleanupInactiveSlotsOptions{
		MinInactiveAge: 1 * time.Hour,
		SlotPrefix:     "rosshield_",
		Logger:         logger,
	}

	removed, err := CleanupInactiveSlots(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("CleanupInactiveSlots: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("want 1 removed, got %d", len(removed))
	}
}

// --- ensureSubscription -----------------------------------------------------

func TestEnsureSubscription_NewlyCreated(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "host=primary port=5432 user=replica dbname=rosshield",
		Copy:              false,
	}

	if err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if got := len(fake.execCalls); got != 1 {
		t.Fatalf("want 1 CREATE exec, got %d", got)
	}
	sqlStmt := fake.lastExecSQL()
	if !strings.Contains(sqlStmt, "CREATE SUBSCRIPTION") {
		t.Fatalf("missing CREATE SUBSCRIPTION: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, `"rosshield_main_sub"`) {
		t.Fatalf("missing quoted name: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, `PUBLICATION "rosshield_main"`) {
		t.Fatalf("missing PUBLICATION ref: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, "copy_data = false") {
		t.Fatalf("missing copy_data = false: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, "create_slot = true") {
		t.Fatalf("missing create_slot = true: %q", sqlStmt)
	}
	if !strings.Contains(sqlStmt, "enabled = true") {
		t.Fatalf("missing enabled = true: %q", sqlStmt)
	}
}

func TestEnsureSubscription_AlreadyExists_Skip(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{true}}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "host=primary",
	}

	if err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if got := len(fake.execCalls); got != 0 {
		t.Fatalf("want 0 CREATE exec (skip), got %d", got)
	}
}

func TestEnsureSubscription_CopyTrue(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "host=primary",
		Copy:              true,
	}

	if err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !strings.Contains(fake.lastExecSQL(), "copy_data = true") {
		t.Fatalf("missing copy_data = true: %q", fake.lastExecSQL())
	}
}

func TestEnsureSubscription_EmptyConnString_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "",
	}

	err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec)
	if !errors.Is(err, ErrEmptyConnString) {
		t.Fatalf("want ErrEmptyConnString, got %v", err)
	}
}

func TestEnsureSubscription_InvalidName_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	spec := SubscriptionSpec{
		Name:              "bad name",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "host=primary",
	}

	err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec)
	if err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("want invalid identifier error, got %v", err)
	}
}

func TestEnsureSubscription_InvalidPublicationName_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "bad;name",
		PrimaryConnString: "host=primary",
	}

	err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec)
	if err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("want invalid identifier error, got %v", err)
	}
}

func TestEnsureSubscription_ConnStringSingleQuoteEscape(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{boolReturns: []bool{false}}
	spec := SubscriptionSpec{
		Name:              "rosshield_main_sub",
		PublicationName:   "rosshield_main",
		PrimaryConnString: "password='secret'",
	}

	if err := Setup(context.Background(), fake, replication.RoleStandby, nil, &spec); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !strings.Contains(fake.lastExecSQL(), "''secret''") {
		t.Fatalf("conn string single-quote not escaped: %q", fake.lastExecSQL())
	}
}

// --- Drop helpers -----------------------------------------------------------

func TestDropPublication_Idempotent(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	if err := DropPublication(context.Background(), fake, "rosshield_main"); err != nil {
		t.Fatalf("DropPublication: %v", err)
	}
	if !strings.Contains(fake.lastExecSQL(), "DROP PUBLICATION IF EXISTS") {
		t.Fatalf("missing IF EXISTS: %q", fake.lastExecSQL())
	}
}

func TestDropSubscription_Idempotent(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	if err := DropSubscription(context.Background(), fake, "rosshield_main_sub"); err != nil {
		t.Fatalf("DropSubscription: %v", err)
	}
	if !strings.Contains(fake.lastExecSQL(), "DROP SUBSCRIPTION IF EXISTS") {
		t.Fatalf("missing IF EXISTS: %q", fake.lastExecSQL())
	}
}

func TestDropPublication_InvalidName_Error(t *testing.T) {
	t.Parallel()
	fake := &fakeExecutor{}
	if err := DropPublication(context.Background(), fake, ""); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("want ErrEmptyName, got %v", err)
	}
}

// --- quote / validate helpers ----------------------------------------------

func TestQuoteIdent_Escape(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"foo":            `"foo"`,
		`weird"x`:        `"weird""x"`,
		"rosshield_main": `"rosshield_main"`,
	}
	for input, want := range cases {
		if got := quoteIdent(input); got != want {
			t.Errorf("quoteIdent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEscapeConnString(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":               "",
		"host=primary":   "host=primary",
		"password='abc'": "password=''abc''",
		"x'y'z":          "x''y''z",
	}
	for input, want := range cases {
		if got := escapeConnString(input); got != want {
			t.Errorf("escapeConnString(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	ok := []string{"foo", "Foo_Bar", "rosshield_main", "x123"}
	for _, s := range ok {
		if err := validateName(s); err != nil {
			t.Errorf("validateName(%q) unexpected error: %v", s, err)
		}
	}
	bad := []string{"", "drop;table", "weird name", `inj"ect`, "with-dash", "tbl.name"}
	for _, s := range bad {
		if err := validateName(s); err == nil {
			t.Errorf("validateName(%q) want error, got nil", s)
		}
	}
}

// --- Defaults --------------------------------------------------------------

func TestDefaultPublicationSpec(t *testing.T) {
	t.Parallel()
	spec := DefaultPublicationSpec()
	if spec.Name != "rosshield_main" {
		t.Errorf("default pub name: %q", spec.Name)
	}
	if !spec.AllTables {
		t.Errorf("default AllTables: want true")
	}
}

func TestDefaultSubscriptionSpec(t *testing.T) {
	t.Parallel()
	spec := DefaultSubscriptionSpec("host=primary")
	if spec.Name != "rosshield_main_sub" {
		t.Errorf("default sub name: %q", spec.Name)
	}
	if spec.PublicationName != "rosshield_main" {
		t.Errorf("default publication ref: %q", spec.PublicationName)
	}
	if spec.PrimaryConnString != "host=primary" {
		t.Errorf("conn string not propagated: %q", spec.PrimaryConnString)
	}
	if spec.Copy {
		t.Errorf("default Copy: want false")
	}
}

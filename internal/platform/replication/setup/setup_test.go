package setup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/replication"
)

// fakeExecutor는 Exec/QueryBool 호출을 기록하는 in-memory test fixture입니다.
type fakeExecutor struct {
	mu sync.Mutex

	// boolReturns: 다음 QueryBool 호출이 반환할 값 (FIFO queue). 비어있으면 false.
	boolReturns []bool
	boolErrs    []error

	// execErr: 모든 Exec 호출이 반환할 에러 (nil이면 성공).
	execErr error

	// 기록.
	execCalls      []execCall
	queryBoolCalls []queryCall
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

package database

import "testing"

func TestBindNamedSkipsLiteralsCommentsAndCasts(t *testing.T) {
	sqlText := `SELECT ':literal', col::text, data ? 'key' FROM t WHERE a=:a /* :ignored */ AND b=':no'`
	got, args, err := bindNamed(sqlText, map[string]interface{}{"a": 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != 7 {
		t.Fatalf("args=%#v", args)
	}
	if got == "" || got[:6] != "SELECT" {
		t.Fatalf("sql=%q", got)
	}
}

func TestBindNamedRequiresEveryParameter(t *testing.T) {
	if _, _, err := bindNamed("select :missing", nil); err == nil {
		t.Fatal("expected missing parameter error")
	}
	if _, _, err := bindNamed("select 1", map[string]interface{}{"unused": 1}); err == nil {
		t.Fatal("expected unused parameter error")
	}
}

func TestBindPositionalMySQL(t *testing.T) {
	got, args, err := bindSQL("mysql", "select * from t where a=? and b=?", nil, []interface{}{1, "x"}, "positional")
	if err != nil {
		t.Fatal(err)
	}
	if got != "select * from t where a=? and b=?" || len(args) != 2 {
		t.Fatalf("sql=%q args=%#v", got, args)
	}
}

func TestBindPositionalRejectedForPostgres(t *testing.T) {
	if _, _, err := bindSQL("postgres", "select * from t where a=?", nil, []interface{}{1}, "positional"); err == nil {
		t.Fatal("expected postgres positional reject")
	}
}

func TestBindNamedDialectDoesNotRewriteQuestionMarkOperator(t *testing.T) {
	got, args, err := bindForDialect("postgres", "select data ? 'key' and id=:id", map[string]interface{}{"id": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got != "select data ? 'key' and id=$1" || len(args) != 1 || args[0] != 3 {
		t.Fatalf("sql=%q args=%#v", got, args)
	}
}

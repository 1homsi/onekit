package genrust

import "testing"

const rustRecursiveOneofFixture = `
package app
message Num { value: int32 }
message Expr {
  node: oneof {
    num: Num @tag("num")
    negate: Expr @tag("not")
    group: Group @tag("group")
  }
}
message Group { inner: oneof { expr: Expr @tag("expr") } }
service Calc { eval(Expr) -> Num @post("/eval") }
`

const rustRecursiveOneofMain = `mod generated;
use generated::types::{Expr, ExprNode, Group, GroupInner, Num};

fn main() {
    let leaf = Expr { node: Some(ExprNode::Num(Num { value: 7 })) };
    let grouped = Group { inner: Some(GroupInner::Expr(Box::new(leaf))) };
    let expr = Expr { node: Some(ExprNode::Negate(Box::new(Expr { node: Some(ExprNode::Group(Box::new(grouped))) }))) };
    let json = serde_json::to_string(&expr).unwrap();
    assert_eq!(json, r#"{"node":{"negate":{"node":{"group":{"inner":{"expr":{"node":{"num":{"value":7},"type":"num"}},"type":"expr"}},"type":"group"}},"type":"not"}}"#);
    let back: Expr = serde_json::from_str(&json).unwrap();
    assert_eq!(back, expr);
    back.validate().unwrap();
    println!("OK");
}
`

func TestGeneratedRustBoxesRecursiveOneofVariants(t *testing.T) {
	runRustWSCrate(t, rustRecursiveOneofFixture, "onekit-rust-recursive-oneof", rustRecursiveOneofMain, true)
}

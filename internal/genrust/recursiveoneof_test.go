package genrust

import "testing"

const rustRecursiveOneofFixture = `namespace app;

model Num {
  value: int32;
}

union ExprNode {
  @Onkt.tag("num") num: Num,
  @Onkt.tag("not") negate: Expr,
  @Onkt.tag("group") group: Group,
}

model Expr {
  node: ExprNode;
}

union GroupInner {
  @Onkt.tag("expr") expr: Expr,
}

model Group {
  inner: GroupInner;
}

interface Calc {
  @post
  @route("/eval")
  eval(...Expr): Num;
}
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
	t.Parallel()
	runRustWSCrate(t, rustRecursiveOneofFixture, "onekit-rust-recursive-oneof", rustRecursiveOneofMain, true)
}

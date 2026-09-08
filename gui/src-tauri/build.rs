fn main() {
    prost_build::compile_protos(
        &["../../proto/localharness/v1/localharness.proto"],
        &["../../proto/"],
    )
    .unwrap();
    tauri_build::build()
}

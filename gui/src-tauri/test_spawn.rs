use std::process::Command;
use std::os::unix::process::CommandExt;

fn main() {
    let mut cmd = Command::new("ls");
    cmd.process_group(0);
}

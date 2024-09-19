const std = @import("std");
const go = @import("go");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const go_build = go.addExecutable(b, .{
        .name = "prebox",
        .target = target,
        .optimize = optimize,
        .package_path = b.path("cmd/prebox"),
    });

    const run_cmd = go_build.addRunStep();
    if (b.args) |args| {
        run_cmd.addArgs(args);
    }

    const run_step = b.step("run", "Run the app");
    run_step.dependOn(&run_cmd.step);

    go_build.addInstallStep();
}

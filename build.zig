const std = @import("std");

pub fn build(b: *std.Build) void {
    // const target = b.standardTargetOptions(.{});
    // const optimize = b.standardOptimizeOption(.{});

    const prebox_cmd = b.addSystemCommand(&.{
        "go",
        "build",
        "-o",
        "zig-out/bin/prebox",
        "./cmd/prebox/",
    });

    const run_cmd = b.addSystemCommand(&.{"./zig-out/bin/prebox"});
    if (b.args) |args| {
        run_cmd.addArgs(args);
    }

    run_cmd.step.dependOn(&prebox_cmd.step);

    const run_step = b.step("run", "Run the app");
    run_step.dependOn(&run_cmd.step);
}

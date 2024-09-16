const std = @import("std");

pub fn build(b: *std.Build) void {
    // const target = b.standardTargetOptions(.{});
    // const optimize = b.standardOptimizeOption(.{});

    {
        const keywork_cmd = b.addSystemCommand(&.{
            "go",
            "build",
            "-o",
            "zig-out/bin/keywork",
            "./cmd/keywork/",
        });

        const run_cmd = b.addSystemCommand(&.{"./zig-out/bin/keywork"});
        if (b.args) |args| {
            run_cmd.addArgs(args);
        }

        run_cmd.step.dependOn(&keywork_cmd.step);

        const run_step = b.step("run", "Run the app");
        run_step.dependOn(&run_cmd.step);
    }

    {
        const keyworkctl_cmd = b.addSystemCommand(&.{
            "go",
            "build",
            "-o",
            "zig-out/bin/keyworkctl",
            "./cmd/keyworkctl/",
        });

        const run_cmd = b.addSystemCommand(&.{"./zig-out/bin/keyworkctl"});
        if (b.args) |args| {
            run_cmd.addArgs(args);
        }

        run_cmd.step.dependOn(&keyworkctl_cmd.step);

        const run_step = b.step("connect", "Connect to a running server");
        run_step.dependOn(&run_cmd.step);
    }
}

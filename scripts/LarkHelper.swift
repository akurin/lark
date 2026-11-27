// A short-lived macOS app identity for the Python HID client.
// Launched through Launch Services; it never interprets shell input.
import Foundation
import Darwin

private struct Request: Decodable {
    let argv: [String]
}

private enum HelperFailure: LocalizedError {
    case invalidRequest(String)

    var errorDescription: String? {
        switch self {
        case .invalidRequest(let message): return message
        }
    }
}

private func put(_ text: String, at path: String) {
    try? text.write(toFile: path, atomically: true, encoding: .utf8)
}

private func finish(_ code: Int32, in directory: String) {
    let result = ["exit_code": Int(code)]
    if let bytes = try? JSONSerialization.data(withJSONObject: result) {
        try? bytes.write(to: URL(fileURLWithPath: directory).appendingPathComponent("result.json"), options: .atomic)
    }
}

private func run(_ directory: String) throws -> Int32 {
    let fileManager = FileManager.default
    let attributes = try fileManager.attributesOfItem(atPath: directory)
    guard let owner = attributes[.ownerAccountID] as? NSNumber,
          owner.uint32Value == getuid(),
          let permissions = attributes[.posixPermissions] as? NSNumber,
          (permissions.intValue & 0o077) == 0 else {
        throw HelperFailure.invalidRequest("request directory must belong to the current user and be private")
    }
    let requestPath = URL(fileURLWithPath: directory).appendingPathComponent("request.json")
    let request = try JSONDecoder().decode(Request.self, from: Data(contentsOf: requestPath))
    let allowed: Set<String> = ["status", "battery", "info", "firmware", "doctor", "raw", "nc", "mute", "charging"]
    guard let command = request.argv.first, allowed.contains(command), request.argv.count <= 16 else {
        throw HelperFailure.invalidRequest("unsupported command")
    }
    guard let python = Bundle.main.object(forInfoDictionaryKey: "LarkPythonPath") as? String,
          fileManager.isExecutableFile(atPath: python) else {
        throw HelperFailure.invalidRequest("the Lark virtual environment is unavailable; run scripts/install.sh")
    }

    let stdout = try FileHandle(forWritingTo: URL(fileURLWithPath: directory).appendingPathComponent("stdout"))
    let stderr = try FileHandle(forWritingTo: URL(fileURLWithPath: directory).appendingPathComponent("stderr"))
    defer { try? stdout.close(); try? stderr.close() }
    let process = Process()
    process.executableURL = URL(fileURLWithPath: python)
    process.arguments = ["-m", "lark.cli"] + request.argv
    var environment = ProcessInfo.processInfo.environment
    environment["LARK_IN_HELPER"] = "1"
    process.environment = environment
    process.standardOutput = stdout
    process.standardError = stderr
    try process.run()
    _ = fileManager.createFile(atPath: directory + "/ready", contents: Data())

    while process.isRunning {
        if fileManager.fileExists(atPath: directory + "/cancel") {
            process.terminate()
            break
        }
        Thread.sleep(forTimeInterval: 0.05)
    }
    process.waitUntilExit()
    return process.terminationStatus
}

if CommandLine.arguments.count != 2 { exit(4) }
let directory = CommandLine.arguments[1]
do {
    finish(try run(directory), in: directory)
} catch {
    put("lark: Lark Helper: \(error.localizedDescription)\n", at: directory + "/stderr")
    finish(4, in: directory)
}

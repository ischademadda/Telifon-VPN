// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "Telifon",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "TelifonUI", targets: ["TelifonUI"])
    ],
    targets: [
        .executableTarget(
            name: "TelifonUI",
            path: "ui/Sources/TelifonUI",
            linkerSettings: [
                .linkedFramework("AppKit"),
                .linkedFramework("SwiftUI"),
                .linkedFramework("QuartzCore")
            ]
        )
    ]
)

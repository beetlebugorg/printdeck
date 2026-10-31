// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI
import UIKit

/// A picture from the service: a photo or a model of a project. It loads with the session of the
/// API, which has the signed cookies of the files of the project, and tries again every 10 seconds
/// after a failure.
struct ServiceImage: View {
    let url: URL?
    var fill = false

    @State private var image: UIImage?

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .aspectRatio(contentMode: fill ? .fill : .fit)
            }
        }
        .task(id: url) { await load() }
    }

    private func load() async {
        guard let url else {
            image = nil
            return
        }
        while !Task.isCancelled {
            if let (data, resp) = try? await ServiceClient.session.data(from: url),
               (resp as? HTTPURLResponse)?.statusCode == 200, let img = UIImage(data: data) {
                image = img
                return
            }
            try? await Task.sleep(for: .seconds(10))
        }
    }
}

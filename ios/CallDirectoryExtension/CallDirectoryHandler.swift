import CallKit
import SpamFilterKit

final class CallDirectoryHandler: CXCallDirectoryProvider {
    override func beginRequest(with context: CXCallDirectoryExtensionContext) {
        let state: BlocklistState
        do {
            state = try BlocklistStore.makeAppGroupStore().load()
        } catch {
            // Never crash the extension -- an unavailable App Group container
            // just means an empty block/identification set this reload.
            context.completeRequest()
            return
        }

        let entries = CallDirectoryEntriesBuilder.build(state)
        for number in entries.blocking {
            context.addBlockingEntry(withNextSequentialPhoneNumber: number)
        }
        for entry in entries.identification {
            context.addIdentificationEntry(withNextSequentialPhoneNumber: entry.number, label: entry.label)
        }
        context.completeRequest()
    }
}

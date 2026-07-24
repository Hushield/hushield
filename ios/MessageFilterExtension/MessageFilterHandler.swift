import IdentityLookup
import SpamFilterKit

final class MessageFilterHandler: ILMessageFilterExtension {
}

extension MessageFilterHandler: ILMessageFilterQueryHandling {
    func handle(
        _ queryRequest: ILMessageFilterQueryRequest,
        context: ILMessageFilterExtensionContext,
        completion: @escaping (ILMessageFilterQueryResponse) -> Void
    ) {
        let state: BlocklistState
        do {
            state = try BlocklistStore.makeAppGroupStore().load()
        } catch {
            // Never crash the extension -- no opinion if the shared store
            // can't be read.
            let response = ILMessageFilterQueryResponse()
            response.action = .none
            completion(response)
            return
        }

        let sender = queryRequest.sender ?? ""
        let body = queryRequest.messageBody ?? ""
        let action = MessageClassifier.classify(sender: sender, body: body, state: state)

        let response = ILMessageFilterQueryResponse()
        switch action {
        case .junk:
            response.action = .junk
        case .allow:
            response.action = .allow
        case .none:
            response.action = .none
        }
        completion(response)
    }
}

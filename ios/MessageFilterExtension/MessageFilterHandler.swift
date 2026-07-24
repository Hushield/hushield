import IdentityLookup

final class MessageFilterHandler: ILMessageFilterExtension {
}

extension MessageFilterHandler: ILMessageFilterQueryHandling {
    func handle(
        _ queryRequest: ILMessageFilterQueryRequest,
        context: ILMessageFilterExtensionContext,
        completion: @escaping (ILMessageFilterQueryResponse) -> Void
    ) {
        let response = ILMessageFilterQueryResponse()
        response.action = .allow
        completion(response)
    }
}

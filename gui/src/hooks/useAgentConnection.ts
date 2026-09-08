import { useEffect, useRef } from 'react';
import WebSocket from '@tauri-apps/plugin-websocket';
import { invoke } from '@tauri-apps/api/core';

// This hook connects to a background agent and listens for `message_agent` host tools
export function useAgentConnection(sessionId: string) {
    const wsRef = useRef<WebSocket | null>(null);

    useEffect(() => {
        let mounted = true;
        let ws: WebSocket | null = null;

        const connect = async () => {
            try {
                // Get connection info (port, api key) from Tauri
                const connInfo: any = await invoke('start_harness', { sessionId, target: null });
                if (!mounted) return;

                ws = await WebSocket.connect(`ws://127.0.0.1:${connInfo.port}/`, {
                    headers: { 
                        'x-localharness-api-key': connInfo.api_key,
                        'Connection': 'Upgrade',
                        'Upgrade': 'websocket'
                    }
                });
                wsRef.current = ws;

                // Load protobuf definitions
                const { ServerMessageSchema, ToolResultSchema, ClientMessageSchema, UserMessageSchema, InterruptRequestSchema, ResumeRequestSchema } = await import('../gen/localharness/v1/localharness_pb');
                const { fromBinary, toBinary, create } = await import('@bufbuild/protobuf');

                ws.addListener((serverMsgRaw) => {
                    if (serverMsgRaw.type === 'Binary') {
                        const serverMsg = fromBinary(ServerMessageSchema, new Uint8Array(serverMsgRaw.data));
                        
                        if (serverMsg.payload.case === "stepUpdate") {
                            const step = serverMsg.payload.value;
                            if (step.action?.case === 'hostToolCall' && step.action.value.toolName === 'message_agent' && step.state === 1 /* WAITING */) {
                                const stepIndex = step.action.value.stepIndex;
                                const args = JSON.parse(step.action.value.argsJson);
                                
                                const handleMessage = async () => {
                                    try {
                                        // 1. Pause this agent since they are leaving their desk
                                        const pauseReq = create(InterruptRequestSchema, {});
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "interrupt", value: pauseReq } })))
                                        });

                                        // 2. Set visiting_session_id to animate walking
                                        await invoke('update_visiting_session_id', { sessionId, visitingSessionId: args.conversation_id });
                                        
                                        // 3. Wait for animation to finish (e.g. 3 seconds)
                                        await new Promise(resolve => setTimeout(resolve, 3000));
                                        
                                        // 4. Send message to target agent
                                        const targetConn: any = await invoke('start_harness', { sessionId: args.conversation_id, target: null });
                                        const targetWs = await WebSocket.connect(`ws://127.0.0.1:${targetConn.port}/`, {
                                            headers: { 
                                                'x-localharness-api-key': targetConn.api_key,
                                                'Connection': 'Upgrade',
                                                'Upgrade': 'websocket'
                                            }
                                        });
                                        const taskMsg = create(UserMessageSchema, { content: `Message from a colleague:\n\n${args.message}`, conversationId: args.conversation_id });
                                        await targetWs.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "userMessage", value: taskMsg } })))
                                        });
                                        await targetWs.disconnect();
                                        
                                        // 5. Clear visiting_session_id so they walk back
                                        await invoke('update_visiting_session_id', { sessionId, visitingSessionId: null });
                                        
                                        // 6. Wait for animation back
                                        await new Promise(resolve => setTimeout(resolve, 3000));
                                        
                                        // 7. Resume this agent
                                        const resumeReq = create(ResumeRequestSchema, {});
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "resume", value: resumeReq } })))
                                        });
                                        
                                        // 8. Return success
                                        const res = create(ToolResultSchema, {
                                            stepId: stepIndex.toString(),
                                            toolName: "message_agent",
                                            resultJson: JSON.stringify({ success: true, message: "Message delivered." })
                                        });
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                        });
                                    } catch (err: any) {
                                        console.error("Message delivery failed:", err);
                                        const res = create(ToolResultSchema, {
                                            stepId: stepIndex.toString(),
                                            toolName: "message_agent",
                                            resultJson: JSON.stringify({ success: false, error: err.toString() }),
                                            isError: true
                                        });
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                        });
                                    }
                                };
                                handleMessage();
                            }
                        }
                    }
                });
            } catch (err) {
                console.error("Agent connection failed:", err);
            }
        };

        connect();

        return () => {
            mounted = false;
            if (wsRef.current) {
                wsRef.current.disconnect();
            }
        };
    }, [sessionId]);
}

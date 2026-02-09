#!/bin/bash
# Debate Platform Server Management Script

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PID_FILE="server.pid"
LOG_FILE="server.log"
BINARY="./debate_server"

case "$1" in
    start)
        if [ -f "$PID_FILE" ] && ps -p $(cat "$PID_FILE") > /dev/null 2>&1; then
            echo "❌ Server is already running (PID: $(cat $PID_FILE))"
            exit 1
        fi

        echo "🚀 Starting Debate Platform Server..."
        nohup $BINARY > $LOG_FILE 2>&1 &
        echo $! > $PID_FILE
        sleep 2

        if ps -p $(cat "$PID_FILE") > /dev/null 2>&1; then
            echo "✅ Server started successfully!"
            echo "   PID: $(cat $PID_FILE)"
            echo "   Frontend: http://0.0.0.0:8081"
            echo "   Logs: tail -f $SCRIPT_DIR/$LOG_FILE"
        else
            echo "❌ Server failed to start. Check logs:"
            tail -20 $LOG_FILE
            exit 1
        fi
        ;;

    stop)
        if [ ! -f "$PID_FILE" ]; then
            echo "❌ PID file not found. Server may not be running."
            exit 1
        fi

        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null 2>&1; then
            echo "🛑 Stopping server (PID: $PID)..."
            kill $PID
            sleep 2

            if ps -p $PID > /dev/null 2>&1; then
                echo "⚠️  Server didn't stop gracefully, forcing..."
                kill -9 $PID
            fi

            rm -f "$PID_FILE"
            echo "✅ Server stopped"
        else
            echo "❌ Server is not running"
            rm -f "$PID_FILE"
        fi
        ;;

    restart)
        $0 stop
        sleep 2
        $0 start
        ;;

    status)
        if [ -f "$PID_FILE" ] && ps -p $(cat "$PID_FILE") > /dev/null 2>&1; then
            PID=$(cat "$PID_FILE")
            echo "✅ Server is running"
            echo "   PID: $PID"
            echo "   Memory: $(ps -p $PID -o rss= | awk '{print $1/1024 " MB"}')"
            echo "   Uptime: $(ps -p $PID -o etime= | xargs)"
            echo ""
            echo "📡 Endpoints:"
            echo "   Frontend: http://0.0.0.0:8081"
            echo "   Bot WS:   ws://0.0.0.0:8081/ws/bot"
            echo "   API:      http://0.0.0.0:8081/api/debates"
        else
            echo "❌ Server is not running"
            exit 1
        fi
        ;;

    logs)
        if [ -f "$LOG_FILE" ]; then
            tail -f "$LOG_FILE"
        else
            echo "❌ Log file not found"
            exit 1
        fi
        ;;

    *)
        echo "Debate Platform Server Manager"
        echo ""
        echo "Usage: $0 {start|stop|restart|status|logs}"
        echo ""
        echo "Commands:"
        echo "  start    - Start the server"
        echo "  stop     - Stop the server"
        echo "  restart  - Restart the server"
        echo "  status   - Show server status"
        echo "  logs     - Follow server logs"
        exit 1
        ;;
esac

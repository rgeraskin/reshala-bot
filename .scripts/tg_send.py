#!/usr/bin/env python3
"""
Telegram message sender using Telethon
Usage: tg_send.py <recipient> <message>
"""

import os
import sys
import argparse
import asyncio
from telethon import TelegramClient


def load_config():
    """Load API credentials from environment variables"""
    api_id = os.getenv('TELEGRAM_API_ID')
    api_hash = os.getenv('TELEGRAM_API_HASH')
    phone = os.getenv('TELEGRAM_PHONE')

    if not all([api_id, api_hash, phone]):
        print("Error: Missing required environment variables:")
        print("  TELEGRAM_API_ID")
        print("  TELEGRAM_API_HASH")
        print("  TELEGRAM_PHONE")
        print("\nGet API credentials from: https://my.telegram.org")
        sys.exit(1)

    return api_id, api_hash, phone


async def send_message_async(recipient, message, session_file='tg_session'):
    """Send a message to a recipient"""
    api_id, api_hash, phone = load_config()

    # Create client
    client = TelegramClient(session_file, api_id, api_hash)

    try:
        await client.start(phone=phone)
        print(f"Sending message to {recipient}...")
        await client.send_message(recipient, message)
        print("Message sent successfully!")
    except Exception as e:
        print(f"Error sending message: {e}")
        sys.exit(1)
    finally:
        await client.disconnect()


def send_message(recipient, message, session_file='tg_session'):
    """Wrapper to run async function"""
    asyncio.run(send_message_async(recipient, message, session_file))


def main():
    parser = argparse.ArgumentParser(
        description='Send Telegram messages from command line',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s username "Hello from terminal!"
  %(prog)s @botname "Testing bot"
  %(prog)s +1234567890 "Message to phone number"
  echo "Test message" | %(prog)s username -

Environment variables:
  TELEGRAM_API_ID     - Your API ID from https://my.telegram.org
  TELEGRAM_API_HASH   - Your API hash from https://my.telegram.org
  TELEGRAM_PHONE      - Your phone number (e.g., +1234567890)
        """
    )

    parser.add_argument('recipient', help='Username, phone number, or @botname')
    parser.add_argument('message', help='Message to send (use "-" to read from stdin)')
    parser.add_argument('-s', '--session', default='tg_session',
                       help='Session file name (default: tg_session)')

    args = parser.parse_args()

    # Read message from stdin if "-"
    message = args.message
    if message == '-':
        message = sys.stdin.read().strip()
        if not message:
            print("Error: No message provided via stdin")
            sys.exit(1)

    send_message(args.recipient, message, args.session)


if __name__ == '__main__':
    main()

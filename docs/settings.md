---
title: User Settings
nav_order: 5
has_children: false
---

# User Settings

## Language

Currently you can choose between **English** and **Deutsch** (German) for the application interface. The language change takes effect immediately. Your language preference is also used in the backend for email notifications (e.g., reminder emails). Backend messages (especially errors) always stay in English.


## Date Format

You can choose between the European (DD.MM.YYYY), US (MM/DD/YYYY), and ISO (YYYY-MM-DD) date format. This affects all date displays and also determines the expected input format when entering dates like birthdays.


## Appearance

Choose your preference between light mode and dark mode. This setting is stored locally in your browser and is not synced across devices.


## Custom Field Names

Define custom fields that appear on all your contacts for tracking information that doesn't fit into the standard contact fields. You can change the order of the custom fields, though they will always be displayed after the standard fields.



## Export and Import Data

**Export Data** (Settings › Data) downloads everything in your account as one CSV file: contacts, relationships, activities, notes and reminders, including custom fields. **Export Contacts as VCF** downloads your contacts as vCards, with profile photos, for use in other contact apps.

**Import Data** reads a CSV file made with Export Data back into the account you are signed in to. Use it to restore a backup, to move your data to another account, or to move it to another Meerkat instance.

1. Click **Import CSV** and choose the file.
2. Meerkat checks the file first and shows, for each kind of record, how many will be added, how many are already there and how many will be skipped. Nothing is saved yet.
3. Click **Import** to add the data.

What the import does:

- **Nothing is added twice.** A contact that matches one you already have (same email address, same first and last name, or same phone number) is not created again, and your existing contact is left unchanged. A relationship, activity, note or reminder is only added if your account doesn't already have the same one, so importing the same file again adds nothing.
- **Links are kept.** Relationships, activities, notes and reminders are attached to the right contacts, including contacts that share a name. Notes without a contact (journal entries) stay without a contact.
- **Custom fields** in the file that your account doesn't have yet are added to your custom fields.
- **Reminders whose email was already sent** are not emailed again.
- **Problems are listed, not fatal.** A value that isn't valid, for example a malformed email address, is left out of that contact. A record that can't be imported at all is skipped. Each case is listed as a warning, with its row number in the file.

What does not come back exactly:

- Addresses are exported as a single line, so an imported address ends up entirely in the street field.
- A single email address, phone number, address, website or messaging handle that contains `; ` is split into two entries, because the export uses `; ` to separate entries. The same goes for a circle name that contains `;`.
- Profile photos, note and activity images, employment history and the completion history of reminders are not part of the export, so they cannot be restored. Use the VCF export to move profile photos.

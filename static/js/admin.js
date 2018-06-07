$(document).ready(function() {
    // init auto-collapsing tabs
    $('#admin-tabs').tabCollapse();

    // create user
    initCreateUser();
});

// Initialise the user creation tab.
function initCreateUser() {
    $("#create-user-form").on("submit", function(e) {
        e.preventDefault();

        setButtonProcessing($("#create-user-form button"), true);
        var data = formToJSON($(this));

        performRequest(hostname + "/admin/createuser", "post", data, function(result) {
            result = JSON.parse(result.trim());

            if (result.status === "success") {
                $("#reset-form").fadeOut(200);
                notifier.queueAlert("User '" + result.value + "' created! Ask the user to check their email.", "success");
                $("#create-user-form")[0].reset();
            }
            else if (result.status === "warning") {
                if (result.value === "invalid_forename") {
                    notifier.queueAlert("Please enter a valid forename.", "warning");
                }
                else if (result.value === "invalid_surname") {
                    notifier.queueAlert("Please enter a valid surname.", "warning");
                }
                else if (result.value === "invalid_email") {
                    notifier.queueAlert("Please enter a valid email.", "warning");
                }
                else if (result.value === "account_already_exists") {
                    notifier.queueAlert("An account with that email address already exists.", "warning")
                }
            }
            else {
                debugLog(result);
                notifier.queueAlert("A server error occurred.", "danger");
            }

            setButtonProcessing($("#create-user-form button"), false);
        });
    });
}